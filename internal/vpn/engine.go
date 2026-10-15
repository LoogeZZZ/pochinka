package vpn

import (
	"bytes"
	"fmt"
	"os"
	"sync"

	xlog "github.com/xtls/xray-core/app/log"
	commonlog "github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// Регистрирует все протоколы, транспорты и приложения xray.
	_ "github.com/xtls/xray-core/main/distro/all"
)

// engine — xray-core, запущенный внутри нашего процесса.
type engine struct {
	mu   sync.Mutex
	inst *core.Instance
}

// SetAssetDir говорит xray, где лежат geoip.dat/geosite.dat.
func SetAssetDir(dir string) {
	_ = os.Setenv("XRAY_LOCATION_ASSET", dir)
}

func (e *engine) start(cfgJSON []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inst != nil {
		_ = e.inst.Close()
		e.inst = nil
	}
	cfg, err := serial.LoadJSONConfig(bytes.NewReader(cfgJSON))
	if err != nil {
		return fmt.Errorf("конфиг xray: %w", err)
	}
	inst, err := core.New(cfg)
	if err != nil {
		return fmt.Errorf("xray: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return fmt.Errorf("xray не запустился: %w", err)
	}
	e.inst = inst
	return nil
}

func (e *engine) stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inst != nil {
		_ = e.inst.Close()
		e.inst = nil
	}
}

func (e *engine) running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inst != nil
}

// mainEngine — движок основного подключения (для возврата ему журнала после
// временных экземпляров-замерщиков).
var mainEngine *engine

// reclaimMainLog делает журнал основного экземпляра снова глобальным: xray
// регистрирует обработчик журнала один на процесс, и последний созданный
// экземпляр (замерщик) перехватывает его, а закрывшись — глушит.
func reclaimMainLog() {
	e := mainEngine
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inst == nil {
		return
	}
	if f, ok := e.inst.GetFeature((*xlog.Instance)(nil)).(*xlog.Instance); ok && f != nil {
		commonlog.RegisterHandler(f)
	}
}
