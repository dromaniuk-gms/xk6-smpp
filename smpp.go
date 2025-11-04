package smpp

import (
    "fmt"
    "time"

    "github.com/fiorix/go-smpp/smpp"
    "github.com/fiorix/go-smpp/smpp/pdu/pdufield"
    "github.com/fiorix/go-smpp/smpp/pdu/pdutext"
    "go.k6.io/k6/js/modules"
    "go.k6.io/k6/metrics"
)

type SMPP struct{}

type Session struct {
    tx        *smpp.Transmitter
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
    latency := reg.MustNewMetric("smpp_submit_latency", metrics.Trend)
    successes := reg.MustNewMetric("smpp_submit_success", metrics.Counter)
    failures := reg.MustNewMetric("smpp_submit_failure", metrics.Counter)

    return &SMPPInstance{
        vu:        vu,
        latency:   latency,
        successes: successes,
        failures:  failures,
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
        if status.Status().Error() != nil {
            return nil, status.Status().Error()
        }
    case <-time.After(5 * time.Second):
        return nil, fmt.Errorf("bind timeout")
    }

    return &Session{
        tx:        tx,
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
        Register: smpp.NoDeliveryReceipt,
    }

    resp, err := s.tx.Submit(msg)
    duration := time.Since(start)

    metrics.PushIfNotDone(s.latency, duration.Seconds())
    if err != nil {
        metrics.PushIfNotDone(s.failures, 1)
        return "", err
    }

    metrics.PushIfNotDone(s.successes, 1)
    msgID, _ := resp.Fields[pdufield.MessageID].String()
    return msgID, nil
}

func (s *Session) Close() {
    if s.tx != nil {
        s.tx.Close()
    }
}
