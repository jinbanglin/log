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
    "unsafe"

    "github.com/jinbanglin/bytebufferpool"
    "github.com/micro/go-micro/metadata"
    "github.com/spf13/viper"

    "context"
)

type level = uint8
type coreStatus = uint32

const (
    _DEBUG    level = iota + 1
    _INFO
    _WARN
    _ERR
    _DISASTER
)

const (
    B  = 1 << (10 * iota)
    KB
    MB
    GB
    TB
    PB
)

const (
    OUT_STDOUT = 0x1f
    OUT_FILE   = 0x8b
)

var (
    coreDead    coreStatus = 2 // gLogger is dead
    coreRunning coreStatus = 1 // gLogger is running
)

type Logger struct {
    look            uint32
    link            string
    Path            string
    FileName        string
    file            *os.File
    fileWriter      *bufio.Writer
    timestamp       int
    FileMaxSize     int
    FileBufSize     int
    fileActualSize  int
    Bucket          chan *bytebufferpool.ByteBuffer
    lock            *sync.RWMutex
    closeSignal     chan string
    sigChan         chan os.Signal
    Persist         int
    sendEmail       bool
    ringInterval    int
    ContextTraceKey string
}

func init() {
    gLogger = &Logger{
        look:            coreDead,
        FileBufSize:     2 * MB,
        link:            "moss",
        Path:            filepath.Join(getCurrentDirectory(), "logs"),
        FileMaxSize:     1024 * MB,
        Bucket:          make(chan *bytebufferpool.ByteBuffer, 1024),
        closeSignal:     make(chan string),
        lock:            &sync.RWMutex{},
        sigChan:         make(chan os.Signal),
        Persist:         OUT_STDOUT,
        ringInterval:    500,
        ContextTraceKey: TraceContextKey,
    }
}

const (
    tsLayout = "2006.01.02.15.04.05"
    fmLayout = `20060102150405™`
)

var gLogger *Logger

func ChaosLogger() {
    if viper.GetBool("log.reconfig") {
        gLogger = &Logger{
            look:            coreDead,
            Path:            filepath.Join(viper.GetString("log.filepath"), viper.GetString("log.linkname")),
            link:            viper.GetString("log.linkname"),
            FileMaxSize:     viper.GetInt("log.maxsize") * MB,
            FileBufSize:     viper.GetInt("log.bufsize") * MB,
            Bucket:          make(chan *bytebufferpool.ByteBuffer, viper.GetInt("log.bucketlen")),
            lock:            &sync.RWMutex{},
            closeSignal:     make(chan string),
            sigChan:         make(chan os.Signal),
            sendEmail:       viper.GetBool("log.send_mail"),
            ringInterval:    500,
            ContextTraceKey: TraceContextKey,
        }
        if viper.GetString("log.out") == "file" {
            gLogger.Persist = OUT_FILE
            go poller()
        }
    }
}

func poller() {
    atomic.SwapUint32(&gLogger.look, coreRunning)
    if err := gLogger.loadCurLogFile(); err != nil {
        if err = gLogger.createFile(); err != nil {
            panic(err)
        }
    }
    go gLogger.signalHandler()
    ticker := time.NewTicker(time.Millisecond * time.Duration(gLogger.ringInterval))

    var (
        now  = time.Now()
        next = now.Add(time.Hour * 24)
    )

    next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, next.Location())

DONE:
    for {
        select {
        case <-gLogger.closeSignal:
            ticker.Stop()
            break DONE
        case <-ticker.C:
            if gLogger.fileWriter.Buffered() > 0 {
                gLogger.sync()
            }
        case n := <-gLogger.Bucket:
            gLogger.fileWriter.Write(n.Bytes())
            gLogger.fileActualSize += n.Len()
            if gLogger.rotate() {
                gLogger.fileWriter.Reset(gLogger.file)
            }
            gLogger.release(n)
        }
    }
}

func (l *Logger) loadCurLogFile() error {
    l.link = filepath.Join(l.Path, gLogger.link+".log")
    actFileName, ok := isLinkFile(l.link)
    if !ok {
        return errors.New(l.link + " is not link file or not exist")
    }
    l.FileName = actFileName
    f, err := openFile(l.FileName)
    if err != nil {
        return err
    }
    info, err := os.Stat(l.FileName)
    if err != nil {
        return err
    }
    t, err := time.Parse(tsLayout, strings.TrimSuffix(path.Base(info.Name()), ".log"))
    if err != nil {
        fmt.Printf("Parse |err=%v \n", err)
        return err
    }
    y, m, d := t.Date()
    l.timestamp = y*10000 + int(m)*100 + d*1
    l.file = f
    l.fileActualSize = int(info.Size())
    l.fileWriter = bufio.NewWriterSize(f, l.FileBufSize)
    return nil
}

func (l *Logger) createFile() (err error) {
    if !pathIsExist(l.Path) {
        if err = os.MkdirAll(l.Path, os.ModePerm); err != nil {
            fmt.Printf("MkdirAll |err=%v \n ", err)
            return
        }
    }
    now := time.Now()
    y, m, d := now.Date()
    l.timestamp = y*10000 + int(m)*100 + d*1
    l.FileName = filepath.Join(
        l.Path,
        now.Format(tsLayout)+".log")
    f, err := openFile(l.FileName)
    if err != nil {
        fmt.Printf("openFile |err=%v \n", err)
        return err
    }
    l.file = f
    l.fileActualSize = 0
    l.fileWriter = bufio.NewWriterSize(f, l.FileBufSize)
    os.Remove(l.link)
    return os.Symlink(l.FileName, l.link)
}

func (l *Logger) sync() {
    if l.lookRunning() {
        err := l.fileWriter.Flush()
        if err != nil {
            fmt.Printf("sync |err=%v \n", err)
        }
    }
}

const fileMaxDelta = 100

func (l *Logger) rotate() bool {
    if !l.lookRunning() {
        return false
    }
    y, m, d := time.Now().Date()
    timestamp := y*10000 + int(m)*100 + d*1
    if l.fileActualSize <= l.FileMaxSize-fileMaxDelta && timestamp <= l.timestamp {
        return false
    }
    l.sync()
    closeFile(l.file)
    return l.createFile() == nil
}

func (l *Logger) lookRunning() bool { return atomic.LoadUint32(&l.look) == coreRunning }

func (l *Logger) lookDead() bool { return atomic.LoadUint32(&l.look) == coreDead }

func (l *Logger) signalHandler() {
    signal.Notify(
        l.sigChan,
        os.Interrupt,
        syscall.SIGINT, // register that too, it should be ok
        // os.Kill等同于syscall.Kill
        os.Kill,
        syscall.SIGKILL, // register that too, it should be ok
        // kill -SIGTERM XXXX
        syscall.SIGTERM,
        syscall.SIGQUIT,
    )

    for {
        select {
        case sig := <-l.sigChan:
            l.closeSignal <- "close"
            fmt.Println("❀ log receive os signal is ", sig)
            l.sync()
            closeFile(l.file)
            atomic.SwapUint32(&l.look, coreDead)
            close(l.Bucket)
            fmt.Println("❀ log shutdown done success")
            os.Exit(1)
        }
    }
}

func (l *Logger) release(buf *bytebufferpool.ByteBuffer) { bytebufferpool.Put(buf) }

func caller() string {
    pc, f, l, _ := runtime.Caller(2)
    funcName := runtime.FuncForPC(pc).Name()
    return path.Base(f) + "/" + path.Base(funcName) + " [" + strconv.Itoa(l) + "] "
}

func flow(lvl level, buf *bytebufferpool.ByteBuffer) {
    if gLogger.sendEmail && lvl >= _ERR {
        EmailInstance().SendMail(buf.String())
    }
    switch gLogger.Persist {
    case OUT_FILE:
        gLogger.Bucket <- buf
    case OUT_STDOUT:
        fmt.Print(buf.String())
    default:
        fmt.Print(buf.String())
    }
}

func Debugf(format string, msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[DEBU] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_DEBUG, buf)
}

func Infof(format string, msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[INFO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_INFO, buf)
}

func Warnf(format string, msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[WARN] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_WARN, buf)
}

func Errorf(format string, msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[ERRO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_ERR, buf)
}

func Fatalf(format string, msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[FTAL] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_DISASTER, buf)
}

func Stackf(format string, msg ...interface{}) {
    s := fmt.Sprintf(format, msg...)
    s += "\n"
    buf := make([]byte, 1<<20)
    n := runtime.Stack(buf, true)
    s += string(buf[:n])
    s += "\n"
    fmt.Println("[STAC][" + time.Now().Format(fmLayout) + "]" + "[" + caller() + "] ❀ " + s)
}

func Debug(msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[DEBU] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_DEBUG, buf)
}

func Info(msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[INFO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_INFO, buf)
}

func Warn(msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[WARN] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_WARN, buf)
}

func Error(msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[ERRO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_ERR, buf)
}

func Fatal(msg ...interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[FTAL] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_DISASTER, buf)
}

func Stack(msg ...interface{}) {
    s := fmt.Sprintln(msg...)
    s += "\n"
    buf := make([]byte, 1<<20)
    n := runtime.Stack(buf, true)
    s += string(buf[:n])
    s += "\n"
    fmt.Println("[STAC][" + time.Now().Format(fmLayout) + "]" + "[" + caller() + "] ❀ " + s)
}

const TraceContextKey = "X-Api-Trace"

func Debugf2(ctx context.Context, format string, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[DEBU] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_DEBUG, buf)
}

func Infof2(ctx context.Context, format string, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[INFO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_INFO, buf)
}

func Warnf2(ctx context.Context, format string, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[WARN] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_WARN, buf)
}

func Errorf2(ctx context.Context, format string, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[ERRO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_ERR, buf)
}

func Fatalf2(ctx context.Context, format string, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[FTAL] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintf(format, msg...) + "\n"))
    flow(_DISASTER, buf)
}

func Debug2(ctx context.Context, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[DEBU] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_DEBUG, buf)
}

func Info2(ctx context.Context, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[INFO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_INFO, buf)
}

func Warn2(ctx context.Context, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[WARN] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_WARN, buf)
}

func Error2(ctx context.Context, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[ERRO] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_ERR, buf)
}

func Fatal2(ctx context.Context, msg ... interface{}) {
    buf := bytebufferpool.Get()
    buf.Write(string2Byte("[FTAL] " + time.Now().Format(fmLayout) + " " + caller() + "❀ "))
    buf.Write(string2Byte(TraceContextKey + "=" + getContextValue(ctx) + " |"))
    buf.Write(string2Byte(fmt.Sprintln(msg...)))
    flow(_DISASTER, buf)
}

func string2Byte(s string) []byte {
    return *(*[]byte)(unsafe.Pointer(&s))
}

func getContextValue(ctx context.Context) string {
    meta, _ := metadata.FromContext(ctx)
    if value, ok := meta[TraceContextKey]; ok {
        return value
    }
    return ""
}

func isLinkFile(filename string) (name string, ok bool) {
    fi, err := os.Lstat(filename)
    if err != nil {
        return
    }

    if fi.Mode()&os.ModeSymlink != 0 {
        name, err = os.Readlink(filename)
        if err != nil {
            fmt.Printf("Readlink |err=%v \n", err)
            return
        }
        return name, true
    } else {
        return
    }
}

func openFile(name string) (file *os.File, err error) {
    file, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND|os.O_SYNC, 0777)
    if err != nil {
        fmt.Printf("openFile err=%v \n", err)
        return
    }
    // syscall.Syscall(syscall.O_SYNC, file.Fd(), 0, 0)
    return
}

func closeFile(file *os.File) {
    if file != nil {
        err := file.Close()
        if err != nil {
            fmt.Printf("closeFile err=%v \n", err)
        }
    }
}

func pathIsExist(path string) bool {
    if _, err := os.Stat(path); err == nil {
        return true
    } else {
        if os.IsNotExist(err) {
            return false
        }
    }
    return false
}

func substr(s string, pos, length int) string {
    runes := []rune(s)
    l := pos + length
    if l > len(runes) {
        l = len(runes)
    }
    return string(runes[pos:l])
}

func getParentDirectory(directory string) string {
    return substr(directory, 0, strings.LastIndex(directory, "/"))
}

func getCurrentDirectory() string {
    ex, err := os.Executable()
    if err != nil {
        panic(err)
    }
    return filepath.Dir(ex)
}
