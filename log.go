package log

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"github.com/jinbanglin/bytesbuffpool"
	"unsafe"
)

var gLogger *Logger

func init() {
	SetupMossLog()
}

func SetupMossLog() {
	setupConfig()
	y, m, d := time.Now().Date()
	gLogger = &Logger{
		look:        uint32(coreDead),
		fileName:    gSetFilename,
		fileBufSize: gSetBufSize,
		path:        filepath.Join(gSetFilePath, gSetFilename),
		timestamp:   y*10000 + int(m)*100 + d*1,
		fileMaxSize: gSetMaxSize,
		bucket:      make(chan *bytesbufferpool.ByteBuffer, gSetBucketLen),
		closeSignal: make(chan string),
		lock:        &sync.RWMutex{},
		sigChan:     make(chan os.Signal),
	}
	if gSetOut == OUT_FILE {
		go poller()
	}
}

func (l *Logger) loadCurLogFile() error {
	l.link = filepath.Join(l.path, gSetFilename+".log")
	actFileName, ok := isLinkFile(l.link)
	if !ok {
		return errors.New("is not link file")
	}
	l.fileName = filepath.Join(l.path, actFileName)
	f, err := openFile(l.fileName)
	if err != nil {
		return err
	}
	info, err := os.Stat(l.fileName)
	if err != nil {
		return err
	}
	sp := strings.Split(actFileName, ".")
	t, err := time.Parse("2006.01.02", sp[1])
	if err != nil {
		fmt.Errorf("loadCurrentLogFile |err=%v", err)
		return err
	}
	y, m, d := t.Date()
	l.timestamp = y*10000 + int(m)*100 + d*1
	l.file = f
	l.fileActualSize = int(info.Size())
	l.fileWriter = bufio.NewWriterSize(f, l.fileBufSize)
	return nil
}

func (l *Logger) createFile() (err error) {
	if !pathIsExist(l.path) {
		if err = os.MkdirAll(l.path, os.ModePerm); err != nil {
			return
		}
	}
	now := time.Now()
	y, m, d := now.Date()
	l.timestamp = y*10000 + int(m)*100 + d*1
	l.fileName = filepath.Join(
		l.path,
		filepath.Base(os.Args[0])+"."+now.Format("2006.01.02.15:04:05")+".log")
	f, err := openFile(l.fileName)
	if err != nil {
		return err
	}
	l.file = f
	l.fileActualSize = 0
	l.fileWriter = bufio.NewWriterSize(f, l.fileBufSize)
	l.link = filepath.Join(l.path, gSetFilename+".log")
	return createLinkFile(l.fileName, l.link)
}

func (l *Logger) sync() {
	if l.lookRunning() {
		l.fileWriter.Flush()
	}
}

const fileMaxDelta = 100

func (l *Logger) rotate() bool {
	if !l.lookRunning() {
		return false
	}
	y, m, d := time.Now().Date()
	timestamp := y*10000 + int(m)*100 + d*1
	if l.fileActualSize <= l.fileMaxSize-fileMaxDelta && timestamp <= l.timestamp {
		return false
	}
	l.fileWriter.Flush()
	closeFile(l.file)
	return l.createFile() == nil
}

func (l *Logger) lookRunning() bool { return atomic.LoadUint32(&l.look) == uint32(coreRunning) }

func (l *Logger) lookDead() bool { return atomic.LoadUint32(&l.look) == uint32(coreDead) }

func (l *Logger) lookBlock() bool { return atomic.LoadUint32(&l.look) == uint32(coreBlock) }

func (l *Logger) signalHandler() {
	signal.Notify(l.sigChan, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case sig := <-l.sigChan:
			l.closeSignal <- "close"
			fmt.Println("receive os signal is ", sig)
			l.fileWriter.Flush()
			closeFile(l.file)
			atomic.SwapUint32(&l.look, uint32(coreDead))
			close(l.bucket)
			os.Exit(1)
		}
	}
}

func (l *Logger) release(buf *bytesbufferpool.ByteBuffer) { bytesbufferpool.Put(buf) }

func caller() string {
	pc, f, l, _ := runtime.Caller(2)
	funcName := runtime.FuncForPC(pc).Name()
	return path.Base(f) + "/" + path.Base(funcName) + " [" + strconv.Itoa(l) + "] "
}

func print(buf *bytesbufferpool.ByteBuffer) {
	switch gSetOut {
	case OUT_FILE:
		gLogger.bucket <- buf
	case OUT_STDOUT:
		fmt.Print(buf.String())
	default:
		fmt.Print(buf.String())
	}
}

func Debugf(format string, msg ... interface{}) {
	if gSetLevel > _DEBUG {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[DEBU] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
	print(buf)
}

func Infof(format string, msg ... interface{}) {
	if gSetLevel > _INFO {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[INFO] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
	print(buf)
}

func Warnf(format string, msg ... interface{}) {
	if gSetLevel > _WARN {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[WARN] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
	print(buf)
}

func Errorf(format string, msg ... interface{}) {
	if gSetLevel > _ERR {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[ERRO] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
	print(buf)
}

func Fatalf(format string, msg ... interface{}) {
	if gSetLevel > _DISASTER {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[FTAL] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
	print(buf)
}

func Stackf(format string, msg ... interface{}) {
	s := fmt.Sprintf(format, msg...)
	s += "\n"
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	s += string(buf[:n])
	s += "\n"
	fmt.Println("[STAC][" + time.Now().Format("01/02/15:04:05") + "]" + "[" + caller() + "] ❀ " + s)
}

func Debug(msg ... interface{}) {
	if gSetLevel > _DEBUG {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[DEBU] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintln(msg...)))
	print(buf)
}

func Info(msg ... interface{}) {
	if gSetLevel > _INFO {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[INFO] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintln(msg...)))
	print(buf)
}

func Warn(msg ... interface{}) {
	if gSetLevel > _WARN {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[WARN] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintln(msg...)))
	print(buf)
}

func Error(msg ... interface{}) {
	if gSetLevel > _ERR {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[ERRO] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintln(msg...)))
	print(buf)
}

func Fatal(msg ... interface{}) {
	if gSetLevel > _DISASTER {
		return
	}
	buf := bytesbufferpool.Get()
	buf.Write(string2Byte("[FTAL] " + time.Now().Format("01/02/15:04:05") + " " + caller() + "❀ "))
	buf.Write(string2Byte(fmt.Sprintln(msg...)))
	print(buf)
}

func Stack(msg ... interface{}) {
	s := fmt.Sprintln(msg...)
	s += "\n"
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	s += string(buf[:n])
	s += "\n"
	fmt.Println("[STAC][" + time.Now().Format("01/02/15:04:05") + "]" + "[" + caller() + "] ❀ " + s)
}

func string2Byte(s string) []byte {
	return *(*[]byte)(unsafe.Pointer(&s))
}
