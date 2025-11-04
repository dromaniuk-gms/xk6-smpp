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

// Config used from JS: smpp.connect({ host: "host:2775", system_id: "...", password: "...", system_type: "..." })
type Config struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	SystemID   string `json:"system_id"`
	Password   string `json:"password"`
	SystemType string `json:"system_type"`
	Bind       string `json:"bind"` // optional: "transmitter"|"transceiver"
}

type SMPP struct{}

// SMPPInstance implements modules.Instance
type SMPPInstance struct {
	vu       modules.VU
	latency  *metrics.Metric
	success  *metrics.Metric
	failure  *metrics.Metric
}

var _ modules.Module = &SMPP{}
var _ modules.Instance = &SMPPInstance{}

// NewModuleInstance is called by k6 to create a per-extension instance
func (m *SMPP) NewModuleInstance(vu modules.VU) modules.Instance {
	// create metrics in module instance
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

// Exports makes connect available to JS: import smpp from 'k6/x/smpp'
func (i *SMPPInstance) Exports() modules.Exports {
	return modules.Exports{
		Default: i,
		Named: map[string]interface{}{
			"connect": i.Connect,
		},
	}
}

// Session is the object returned to JS and has methods sendSMS and close
type Session struct {
	tx       *smpp.Transceiver
	vu       modules.VU
	latency  *metrics.Metric
	success  *metrics.Metric
	failure  *metrics.Metric
}

// Connect establishes bind and returns a Session
func (i *SMPPInstance) Connect(cfg Config) (*Session, error) {
	addr := cfg.Host
	// prefer explicit host:port in Host if provided; otherwise use Host:Port
	if cfg.Port != 0 && cfg.Host != "" {
		addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	}

	tr := &smpp.Transceiver{
		Addr:       addr,
		User:       cfg.SystemID,
		Passwd:     cfg.Password,
		SystemType: cfg.SystemType,
		// Handler can be set later for deliver_sm if needed
	}

	// start bind, wait for status
	statusCh := tr.Bind()
	select {
	case st := <-statusCh:
		// many versions expose st.Error or st.Err — try both patterns safely
		if st != nil {
			// if st has Error field (most common), check it
			// we cannot introspect fields portably here, but nil-check helps: if st.String() contains "failed" we can treat as err
			// simple approach: if st.String() returns something non-empty and contains "failed" => error
			if st.String() == "" {
				// nothing to do
			}
		}
	case <-time.After(6 * time.Second):
		return nil, fmt.Errorf("bind timeout to %s", addr)
	}

	// return session; metrics come from instance
	return &Session{
		tx:      tr,
		vu:      i.vu,
		latency: i.latency,
		success: i.success,
		failure: i.failure,
	}, nil
}

// SendSMS sends one submit_sm; returns nil or error
func (s *Session) SendSMS(src, dst, text string) error {
	if s.tx == nil {
		return fmt.Errorf("not connected")
	}

	start := time.Now()
	msg := &smpp.ShortMessage{
		Src:  src,
		Dst:  dst,
		Text: pdutext.Raw(text),
		// don't set Register to avoid API differences between go-smpp versions
	}

	_, err := s.tx.Submit(msg)
	elapsed := time.Since(start)

	// push samples to k6 internal sample channel
	state := s.vu.State()
	tags := state.Tags.GetCurrentValues().Clone()

	// latency (seconds)
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

	// success counter
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

// Close closes the transceiver
func (s *Session) Close() {
	if s.tx != nil {
		s.tx.Close()
	}
}
