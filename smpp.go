package smpp

import (
	"fmt"
	"time"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdutext"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

func init() {
	modules.Register("k6/x/smpp", new(SMPP))
}

// Config is used from JS: smpp.connect({ host, port, system_id, password, system_type })
type Config struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	SystemID   string `json:"system_id"`
	Password   string `json:"password"`
	SystemType string `json:"system_type"`
	Bind       string `json:"bind"`
}

type SMPP struct{}

// SMPPInstance implements modules.Instance
type SMPPInstance struct {
	vu      modules.VU
	latency *metrics.Metric
	success *metrics.Metric
	failure *metrics.Metric
}

var _ modules.Module = &SMPP{}
var _ modules.Instance = &SMPPInstance{}

func (m *SMPP) NewModuleInstance(vu modules.VU) modules.Instance {
	reg := vu.InitEnv().Registry
	return &SMPPInstance{
		vu:      vu,
		latency: reg.MustNewMetric("smpp_submit_latency", metrics.Trend),
		success: reg.MustNewMetric("smpp_submit_success", metrics.Counter),
		failure: reg.MustNewMetric("smpp_submit_failure", metrics.Counter),
	}
}

func (i *SMPPInstance) Exports() modules.Exports {
	return modules.Exports{
		Default: i,
		Named: map[string]interface{}{
			"connect": i.Connect,
		},
	}
}

// Session returned to JS
type Session struct {
	tx      *smpp.Transceiver
	vu      modules.VU
	latency *metrics.Metric
	success *metrics.Metric
	failure *metrics.Metric
}

func (i *SMPPInstance) Connect(cfg Config) (*Session, error) {
	addr := cfg.Host
	if cfg.Port != 0 && cfg.Host != "" {
		addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	}

	tr := &smpp.Transceiver{
		Addr:       addr,
		User:       cfg.SystemID,
		Passwd:     cfg.Password,
		SystemType: cfg.SystemType,
	}

	statusCh := tr.Bind()
	select {
	case st := <-statusCh:
		if st != smpp.Connected {
			return nil, fmt.Errorf("bind failed, status: %v", st)
		}
	case <-time.After(6 * time.Second):
		return nil, fmt.Errorf("bind timeout to %s", addr)
	}

	return &Session{
		tx:      tr,
		vu:      i.vu,
		latency: i.latency,
		success: i.success,
		failure: i.failure,
	}, nil
}

func (s *Session) SendSMS(src, dst, text string) error {
	if s.tx == nil {
		return fmt.Errorf("not connected")
	}

	start := time.Now()
	msg := &smpp.ShortMessage{
		Src:  src,
		Dst:  dst,
		Text: pdutext.Raw([]byte(text)),
	}

	_, err := s.tx.Submit(msg)
	elapsed := time.Since(start)

	state := s.vu.State()
	tags := state.Tags.GetCurrentValues().Tags // ✅ потрібен саме .Tags

	state.Samples <- metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: s.latency,
			Tags:   tags,
		},
		Value: float64(elapsed.Seconds()),
		Time:  time.Now(),
	}

	if err != nil {
		state.Samples <- metrics.Sample{
			TimeSeries: metrics.TimeSeries{
				Metric: s.failure,
				Tags:   tags,
			},
			Value: 1,
			Time:  time.Now(),
		}
		return err
	}

	state.Samples <- metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: s.success,
			Tags:   tags,
		},
		Value: 1,
		Time:  time.Now(),
	}

	return nil
}

func (s *Session) Close() {
	if s.tx != nil {
		s.tx.Close()
	}
}
