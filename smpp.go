package smpp

import (
	"fmt"
	"time"

	"github.com/linxGnu/gosmpp"
	"github.com/linxGnu/gosmpp/pdu"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

func init() {
	modules.Register("k6/x/smpp", new(SMPP))
}

type Config struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	SystemID   string `json:"system_id"`
	Password   string `json:"password"`
	SystemType string `json:"system_type"`
}

type SMPP struct{}

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
	lat := reg.MustNewMetric("smpp_submit_latency", metrics.Trend)
	suc := reg.MustNewMetric("smpp_submit_success", metrics.Counter)
	fail := reg.MustNewMetric("smpp_submit_failure", metrics.Counter)

	return &SMPPInstance{
		vu:      vu,
		latency: lat,
		success: suc,
		failure: fail,
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

type Session struct {
	client  *gosmpp.Transceiver
	vu      modules.VU
	latency *metrics.Metric
	success *metrics.Metric
	failure *metrics.Metric
}

// Connect establishes a new transceiver bind
func (i *SMPPInstance) Connect(cfg Config) (*Session, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	trx := gosmpp.NewTransceiver()
	trx.Addr = addr
	trx.User = cfg.SystemID
	trx.Passwd = cfg.Password
	trx.SystemType = cfg.SystemType
	trx.Handler = func(p pdu.PDU, err error) {
		if err != nil {
			fmt.Printf("SMPP handler error: %v\n", err)
		}
	}

	if err := trx.Bind(); err != nil {
		return nil, fmt.Errorf("bind failed: %v", err)
	}

	return &Session{
		client:  trx,
		vu:      i.vu,
		latency: i.latency,
		success: i.success,
		failure: i.failure,
	}, nil
}

func (s *Session) SendSMS(src, dst, message string) error {
	if s.client == nil {
		return fmt.Errorf("not connected")
	}

	start := time.Now()

	sm := pdu.NewSubmitSM()
	sm.SourceAddr = src
	sm.DestinationAddr = dst
	sm.ShortMessage = []byte(message)

	if err := s.client.Transmitter().Submit(sm); err != nil {
		s.pushMetric(s.failure, 1)
		return fmt.Errorf("submit error: %v", err)
	}

	s.pushMetric(s.latency, time.Since(start).Seconds())
	s.pushMetric(s.success, 1)
	return nil
}

func (s *Session) pushMetric(metric *metrics.Metric, value float64) {
	state := s.vu.State()
	tags := state.Tags.GetCurrentValues()
	state.Samples <- metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: metric,
			Tags:   &tags,
		},
		Value: value,
		Time:  time.Now(),
	}
}

func (s *Session) Close() {
	if s.client != nil {
		s.client.Close()
	}
}
