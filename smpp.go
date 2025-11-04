package smpp

import (
    "context"
    "fmt"
    "time"

    "github.com/fiorix/go-smpp/smpp"
    "github.com/fiorix/go-smpp/smpp/pdu"
    "github.com/fiorix/go-smpp/smpp/pdu/pdufield"
    "github.com/fiorix/go-smpp/smpp/pdu/pdutext"
    "go.k6.io/k6/js/modules"
    "go.k6.io/k6/metrics"
)

type SMPP struct{}

type Session struct {
    tx        *smpp.Transmitter
    vu        modules.VU
    latency   *metrics.Metric
    successes *metrics.Metric
    failures  *metrics.Metric
}

type Config struct {
    Host       string
    Port       int
    SystemID   string
    Password   string
    SystemType string
}

func init() {
    modules.Register("k6/x/smpp", new(SMPP))
}

type SMPPInstance struct {
    vu        modules.VU
    latency   *metrics.Metric
    successes *metrics.Metric
    failures  *metrics.Metric
}

var _ modules.Module = &SMPP{}
var _ modules.Instance = &SMPPInstance{}

func (m *SMPP) NewModuleInstance(vu modules.VU) modules.Instance {
    reg := vu.InitEnv().Registry
    return &SMPPInstance{
        vu:        vu,
        latency:   reg.MustNewMetric("smpp_submit_latency", metrics.Trend),
        successes: reg.MustNewMetric("smpp_submit_success", metrics.Counter),
        failures:  reg.MustNewMetric("smpp_submit_failure", metrics.Counter),
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

func (i *SMPPInstance) Connect(cfg Config) (*Session, error) {
    addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

    tx := &smpp.Transmitter{
        Addr:       addr,
        User:       cfg.SystemID,
        Passwd:     cfg.Password,
        SystemType: cfg.SystemType,
    }

    conn := tx.Bind()
    select {
    case status := <-conn:
        if status.Err != nil {
            return nil, fmt.Errorf("bind failed: %v", status.Err)
        }
    case <-time.After(5 * time.Second):
        return nil, fmt.Errorf("bind timeout")
    }

    return &Session{
        tx:        tx,
        vu:        i.vu,
        latency:   i.latency,
        successes: i.successes,
        failures:  i.failures,
    }, nil
}

func (s *Session) SendSMS(src, dst, text string) (string, error) {
    if s.tx == nil {
        return "", fmt.Errorf("not connected")
    }

    start := time.Now()
    msg := &smpp.ShortMessage{
        Src:      src,
        Dst:      dst,
        Text:     pdutext.Latin1(text),
        // без Register — старе поле, уже не потрібно
    }

    resp, err := s.tx.Submit(msg)
    duration := time.Since(start)

    state := s.vu.State()
    samples := state.Samples

    // latency
    samples <- metrics.Sample{
        TimeSeries: metrics.TimeSeries{
            Metric: s.latency,
            Tags:   state.Tags.GetCurrentValues(),
        },
        Value: float64(duration.Seconds()),
        Time:  time.Now(),
    }

    if err != nil {
        samples <- metrics.Sample{
            TimeSeries: metrics.TimeSeries{
                Metric: s.failures,
                Tags:   state.Tags.GetCurrentValues(),
            },
            Value: 1,
            Time:  time.Now(),
        }
        return "", err
    }

    // success counter
    samples <- metrics.Sample{
        TimeSeries: metrics.TimeSeries{
            Metric: s.successes,
            Tags:   state.Tags.GetCurrentValues(),
        },
        Value: 1,
        Time:  time.Now(),
    }

    // Витягуємо MessageID із PDU
    var msgID string
    if resp != nil {
        if f := resp.Field(pdufield.MessageID); f != nil {
            msgID, _ = f.String()
        }
    }

    return msgID, nil
}

func (s *Session) Close() {
    if s.tx != nil {
        s.tx.Close()
    }
}
