package log

import (
	"bufio"
	"io"
	"os"
	"sync"
	"github.com/jinbanglin/bytebufferpool"
)

type Hook interface {
	Fire(writer *bufio.Writer)
	Level(level)
}

type Logger struct {
	look           uint32
	link           string
	path           string
	fileName       string
	file           *os.File
	fileWriter     *bufio.Writer
	timestamp      int
	fileMaxSize    int
	fileBufSize    int
	fileActualSize int
	bucket         chan *bytebufferpool.ByteBuffer
	bucketFlushLen int
	lock           *sync.RWMutex
	output         io.Writer
	closeSignal    chan string
	sigChan        chan os.Signal
}
