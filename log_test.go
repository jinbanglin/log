package log_test

import (
	"testing"
	"github.com/jinbanglin/moss/log"
)

func TestErrorf(t *testing.T) {
	log.Errorf("err test=%v", "something error")
}
