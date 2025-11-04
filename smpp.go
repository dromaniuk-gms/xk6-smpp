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
	Bind       string `json:"bind"` // transceiver|transmitter
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
	tx      *gosmpp.Transceiver
	vu      modules.VU
	latency *metrics.Metric
	success *metrics.Metric
	failure *metrics.Metric
}

func (i *SMPPInstance) Connect(cfg Config) (*Session, error) {
	addr := cfg.Host
	if cfg.Port != 0 {
		addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	}

	settings := &gosmpp.TransceiverSettings{
		Address:     addr,
		User:        cfg.SystemID,
		Password:    cfg.Password,
		SystemType:  cfg.SystemType,
		EnquireLink: 10 * time.Second,
		OnSubmitError: func(p pdu.PDU, err error) {
			fmt.Printf("submit error: %v\n", err)
		},
		OnReceivingPDU: func(p pdu.PDU, e error) {
			if e != nil {
				fmt.Printf("recv error: %v\n", e)
			}
		},
	}

	tx, err := gosmpp.NewTransceiver(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %v", err)
	}

	return &Session{
		tx:      tx,
		vu:      i.vu,
		latency: i.latency,
		success: i.success,
		failure: i.failure,
	}, nil
}

func (s *Session) SendSMS(src, dst, msg string) error {
	if s.tx == nil {
		return fmt.Errorf("not connected")
	}

	start := time.Now()

	sm, err := pdu.NewSubmitSM()
	if err != nil {
		return err
	}

	sm.SourceAddrTon = field.TonAlphanumeric
	sm.SourceAddrNpi = field.NpiUnknown
	sm.DestAddrTon = field.TonInternational
	sm.DestAddrNpi = field.NpiISDN
	sm.SourceAddr = field.NewAddress(src)
	sm.DestinationAddr = field.NewAddress(dst)
	sm.ShortMessage = text.Raw(msg)

	err = s.tx.Transceiver().Submit(sm)
	elapsed := time.Since(start)

	state := s.vu.State()
	tags := state.Tags.GetCurrentValues()

	state.Samples <- metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: s.latency,
			Tags:   &tags,
		},
		Value: float64(elapsed.Seconds()),
		Time:  time.Now(),
	}

	if err != nil {
		state.Samples <- metrics.Sample{
			TimeSeries: metrics.TimeSeries{
				Metric: s.failure,
				Tags:   &tags,
			},
			Value: 1,
			Time:  time.Now(),
		}
		return err
	}

	state.Samples <- metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: s.success,
			Tags:   &tags,
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
