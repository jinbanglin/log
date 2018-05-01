package log

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"sync"
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
	bucket         chan *bytes.Buffer
	bucketFlushLen int
	lock           *sync.RWMutex
	output         io.Writer
	closeSignal    chan string
	sigChan        chan os.Signal
}
