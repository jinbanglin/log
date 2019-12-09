package main

import (
    "sync/atomic"
    "time"
    "fmt"
    "github.com/jinbanglin/log"
    "github.com/spf13/viper"
    "github.com/fsnotify/fsnotify"
)

// config toml file
// [log]
// reconfig              =true
// bucketlen             =1024
// linkname              ="srv.cld"
// filepath              ="/root/logs"
// maxsize               =256
// bufsize               =2
// out                   ="file"
// send_mail             =false

// func main() {
// 	var now = time.Now()
//
// 	for j := 0; j < 1000000; j++ {
// 		log.Debugf("tps test |how times log can bear |message=%v", "ahha ahhaa")
// 	}
// 	fmt.Println("time", time.Now().Sub(now).Seconds())
// 	var str string
// 	fmt.Scanln(&str)
//
// 	//OutPut
// 	//time 2.166870248
// }

func Chaos(fn string, f ...func()) {
    viper.SetConfigType("toml")
    viper.SetConfigFile(fn)
    if err := viper.ReadInConfig(); err != nil {
        panic(fmt.Errorf("fatal error config file: %s \n", err))
    }
    for _, v := range f {
        v()
    }
    viper.WatchConfig()
    viper.OnConfigChange(func(e fsnotify.Event) {
        fmt.Println("config file changed:", e.Name)
        for _, v := range f {
            v()
        }
    })
}

func main() {
    Chaos("./example.toml", log.ChaosLogger)

    log.Debugf("tps test |how times log can bear |message=%v", "ahha ahhaa")
    var tps int64 = 0

    for i := 0; i < 100; i++ {
        go func() {
            for j := 0; j < 10000000; j++ {
                log.Debugf("tps test |how times log can bear |message=%v", "ahha ahhaa")
                atomic.AddInt64(&tps, 1)
            }
        }()
    }

    for i := 0; i < 20; i++ {
        time.Sleep(time.Second)
        fmt.Println("tps is ", atomic.LoadInt64(&tps))
        atomic.SwapInt64(&tps, 0)
    }
    // OutPut
    // tps is : 1443402
    //
}

// func main() {
// 	log.Stackf("test |message=%s", "ahhh")
// }

// func main() {
//     log.Debugf("substring=%s%s", "log is a lightweight log to use", "debugf test")
//     log.Infof("message=%s", "log is a lightweight log to use")
//     log.Errorf("message=%s", "log is a lightweight log to use")
//     log.Warnf("message=%s", "log is a lightweight log to use")
//     log.Fatalf("message=%s", "log is a lightweight log to use")
//     log.Stackf("test=%s", "11111")
//     var str string
//     fmt.Scanln(&str)
// }
