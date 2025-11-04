package smpp

import (
    "fmt"
    "log"
    "time"

    "github.com/fiorix/go-smpp/smpp"
    "github.com/fiorix/go-smpp/smpp/pdu/pdufield"
    "github.com/fiorix/go-smpp/smpp/pdu/pdutext"
    "go.k6.io/k6/js/modules"
    "go.k6.io/k6/metrics"
)

type SMPP struct{}

type Config struct {
    Host       string
    Port       int
    SystemID   string
    Password   string
    SystemType string
    Bind       string // "transmitter" | "transceiver"
}

type Session struct {
    tx          *smpp.Transceiver
    submitTrend *metrics.Trend
    dlrTrend    *metrics.Trend
    successes   *metrics.Counter
    failures    *metrics.Counter
}

// K6 instance
func (s *SMPP) XNewModuleInstance(vu modules.VU) modules.Instance {
    rt := vu.InitEnv().Registry
    return &SMPPInstance{
        vu: vu,
        submitTrend: rt.NewTrend("smpp_submit_latency", metrics.TrendTime, metrics.Default),
        dlrTrend:    rt.NewTrend("smpp_dlr_latency", metrics.TrendTime, metrics.Default),
        successes:   rt.NewCounter("smpp_submit_success"),
        failures:    rt.NewCounter("smpp_submit_failure"),
    }
}

type SMPPInstance struct {
    vu          modules.VU
    submitTrend *metrics.Trend
    dlrTrend    *metrics.Trend
    successes   *metrics.Counter
    failures    *metrics.Counter
}

var _ modules.Module = &SMPP{}
var _ modules.Instance = &SMPPInstance{}

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

    // Callback для отримання deliver_sm
    rx := func(pdu smpp.PDU) {
        if pdu == nil {
            return
        }
        if pdu.Header().ID.String() == "deliver_sm" {
            i.dlrTrend.Add(0.001) // фіксуємо факт отримання, latency можна обчислити у майбутньому
            log.Printf("[SMPP] Received deliver_sm: %+v\n", pdu)
        }
    }

    tr := &smpp.Transceiver{
        Addr:       addr,
        User:       cfg.SystemID,
        Passwd:     cfg.Password,
        SystemType: cfg.SystemType,
        Handler:    smpp.HandlerFunc(rx),
    }

    conn := tr.Bind()
    select {
    case status := <-conn:
        if status.Status().Error() != nil {
            return nil, status.Status().Error()
        }
        log.Printf("[SMPP] Connected to %s", addr)
    case <-time.After(5 * time.Second):
        return nil, fmt.Errorf("bind timeout")
    }

    return &Session{
        tx:          tr,
        submitTrend: i.submitTrend,
        dlrTrend:    i.dlrTrend,
        successes:   i.successes,
        failures:    i.failures,
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
        Register: smpp.FinalDeliveryReceipt, // просимо SMSC надіслати DLR
    }

    resp, err := s.tx.Submit(msg)
    duration := time.Since(start)
    s.submitTrend.Add(duration.Seconds())

    if err != nil {
        s.failures.Add(1)
        return "", err
    }

    s.successes.Add(1)
    msgID, _ := resp.Fields[pdufield.MessageID].String()
    return msgID, nil
}

func (s *Session) Close() {
    if s.tx != nil {
        s.tx.Close()
    }
}
