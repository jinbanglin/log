package log

import (
	"os"

	"github.com/spf13/viper"
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
	coreDead    coreStatus = 2 //gLogger is dead
	coreBlock   coreStatus = 0 //gLogger is block
	coreRunning coreStatus = 1 //gLogger is running
)

var gSetOut = OUT_STDOUT
var gSetMaxSize = 256 * MB
var gSetBucketLen = 1024
var gSetBufSize = 2 * MB
var gSetFilename = "moss"
var gSetFilePath = getCurrentDirectory()
var gSetLevel = _DEBUG
var gSetPollerInterval = 500

func setupConfig() {
	if value := viper.GetInt("log.bucketlen"); value > 0 {
		gSetBucketLen = value
	}
	if value := viper.GetString("log.linkname"); value != "" {
		gSetFilename = value
	}
	if value := viper.GetString("log.out"); value != "" {
		switch value {
		case "stdout":
			gSetOut = OUT_STDOUT
		case "file":
			gSetOut = OUT_FILE
		default:
			gSetOut = OUT_STDOUT
		}
	}
	if value := viper.GetString("log.filepath"); value != "" {
		if !pathIsExist(value) &&gSetOut==OUT_FILE{
			if err := os.Mkdir(value, os.ModePerm); err != nil {
				panic(err)
			}
		}
		gSetFilePath = value
	}
	if value := viper.GetInt("log.level"); value > 0 {
		gSetLevel = level(value)
	}
	if value := viper.GetInt("log.maxsize"); value > 0 {
		gSetMaxSize = value * MB
	}
	if value := viper.GetInt("log.interval"); value > 0 {
		gSetPollerInterval = value
	}
}
