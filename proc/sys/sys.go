package sys

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/coreos/go-systemd/daemon"
	restful "github.com/emicklei/go-restful"
	"github.com/yubo/golib/configer"
	"github.com/yubo/golib/logs"
	"github.com/yubo/golib/mail"
	"github.com/yubo/golib/openapi"
	"github.com/yubo/golib/openapi/urlencoded"
	"github.com/yubo/golib/orm"
	"github.com/yubo/golib/proc"
	"github.com/yubo/golib/util"
	"k8s.io/klog/v2"
)

// type {{{

type DbConfig struct {
	Driver string `json:"driver"`
	Dsn    string `json:"dsn"`
}

type Config struct {
	GrpcMaxRecvMsgSize int         `json:"grpcMaxRecvMsgSize"`
	LogLevel           int         `json:"logLevel"`
	WatchdogSec        int         `json:"watchdogSec" description:"The time to feed the dog"`
	Mail               mail.Config `json:"mail"`
}

func (p Config) String() string {
	return util.Prettify(p)
}

func (p *Config) Validate() error {
	if err := p.Mail.Validate(); err != nil {
		return fmt.Errorf("sys: %s", err)
	}

	return nil
}

type Module struct {
	*Config
	oldConfig *Config
	Name      string
	db        *orm.Db
	ctx       context.Context
	cancel    context.CancelFunc
	cpuFd     *os.File
	heapFd    *os.File
}

// }}}

const (
	moduleName = "sys"
)

var (
	EEXIST  = errors.New("Process exists")
	_module = &Module{Name: moduleName}
	hookOps = []proc.HookOps{{
		Hook:     _module.preStart,
		Owner:    moduleName,
		HookNum:  proc.ACTION_START,
		Priority: proc.PRI_PRE_SYS,
	}, {
		Hook:     _module.test,
		Owner:    moduleName,
		HookNum:  proc.ACTION_TEST,
		Priority: proc.PRI_PRE_SYS,
	}, {
		Hook:     _module.start,
		Owner:    moduleName,
		HookNum:  proc.ACTION_START,
		Priority: proc.PRI_SYS,
	}, {
		Hook:     _module.stop,
		Owner:    moduleName,
		HookNum:  proc.ACTION_STOP,
		Priority: proc.PRI_SYS,
	}, {
		Hook:     _module.preStart,
		Owner:    moduleName,
		HookNum:  proc.ACTION_RELOAD,
		Priority: proc.PRI_PRE_SYS,
	}, {
		Hook:     _module.start,
		Owner:    moduleName,
		HookNum:  proc.ACTION_RELOAD,
		Priority: proc.PRI_SYS,
	}}
)

// must include logging
func init() {
	restful.RegisterEntityAccessor(openapi.MIME_URL_ENCODED, urlencoded.NewEntityAccessor())
	proc.RegisterHooks(hookOps)
}

func (p *Module) test(ops *proc.HookOps, configer *configer.Configer) (err error) {
	c := &Config{}
	if err := configer.Read(p.Name, c); err != nil {
		return fmt.Errorf("%s read config err: %s", p.Name, err)
	}
	return c.Validate()
}

func (p *Module) preStart(ops *proc.HookOps, configer *configer.Configer) (err error) {
	if p.cancel != nil {
		p.cancel()
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())

	c := &Config{}
	if err := configer.Read(p.Name, c); err != nil {
		return err
	}

	p.Config, p.oldConfig = c, p.Config

	logs.FlagSet.Set("v", fmt.Sprintf("%d", p.LogLevel))
	klog.Infof("log level %d", p.LogLevel)

	return nil
}

func (p *Module) start(ops *proc.HookOps, configer *configer.Configer) error {
	// watch dog
	if t := p.WatchdogSec; t > 0 {
		daemon.SdNotify(false, "READY=1")
		go util.Until(
			func() {
				daemon.SdNotify(false, "WATCHDOG=1")
			},
			time.Duration(t)*time.Second,
			p.ctx.Done(),
		)
	}

	return nil
}

func (p *Module) stop(ops *proc.HookOps, configer *configer.Configer) error {
	p.cancel()
	return nil
}
