package smpp

import (
	"context"
	"fmt"
	"time"

	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu/pdufield"
	"github.com/fiorix/go-smpp/smpp/pdu/pdutext"
)

// Register extension
func init() {
	modules.Register("k6/x/smpp", new(SMPP))
}

// SMPP represents the module structure
type SMPP struct {
	client  *smpp.Transceiver
	metrics struct {
		Sent     *metrics.Metric
		Received *metrics.Metric
		Errors   *metrics.Metric
	}
}

// NewModuleInstance implements k6 module interface
func (s *SMPP) NewModuleInstance(vu modules.VU) modules.Instance {
	return &SMPPInstance{
		vu: vu,
		s:  s,
	}
}

// SMPPInstance holds per-VU state
type SMPPInstance struct {
	vu modules.VU
	s  *SMPP
}

// Connect to SMPP server
func (s *SMPPInstance) Connect(ctx context.Context, host, user, pass string) error {
	transceiver := &smpp.Transceiver{
		Addr:   host,
		User:   user,
		Passwd: pass,
	}
	client := transceiver.Bind()
	s.s.client = client

	// Register metrics
	registry := s.vu.InitEnv().Registry
	s.s.metrics.Sent = registry.MustNewMetric("smpp_sent_messages", metrics.Counter)
	s.s.metrics.Received = registry.MustNewMetric("smpp_received_messages", metrics.Counter)
	s.s.metrics.Errors = registry.MustNewMetric("smpp_errors", metrics.Counter)

	return nil
}

// SendSMS sends an SMPP message
func (s *SMPPInstance) SendSMS(ctx context.Context, src, dst, text string) error {
	state := common.GetState(ctx)
	if state == nil {
		return fmt.Errorf("no VU state")
	}

	if s.s.client == nil {
		return fmt.Errorf("SMPP client not connected")
	}

	tags := state.Tags.GetCurrentValues().Tags
	start := time.Now()

	msg := &smpp.ShortMessage{
		Src:      src,
		Dst:      dst,
		Text:     pdutext.Raw(text),
		Register: smpp.FinalDeliveryReceipt,
	}

	resp, err := s.s.client.Submit(msg)
	elapsed := time.Since(start).Seconds()

	samples := s.vu.InitEnv().Samples
	if err != nil {
		samples <- metrics.Sample{
			Time:  time.Now(),
			Value: 1,
			Metric: s.s.metrics.Errors,
			Tags:  tags,
		}
		return fmt.Errorf("send error: %w", err)
	}

	if resp != nil && resp.Resp != nil {
		msgID := resp.Resp.Field(pdufield.MessageID)
		fmt.Printf("[k6/x/smpp] Sent message id: %v\n", msgID)
	}

	samples <- metrics.Sample{
		Time:  time.Now(),
		Value: 1,
		Metric: s.s.metrics.Sent,
		Tags:  tags,
	}

	fmt.Printf("[k6/x/smpp] Message sent to %s in %.2fs\n", dst, elapsed)
	return nil
}

// Close disconnects from SMPP
func (s *SMPPInstance) Close() {
	if s.s.client != nil {
		s.s.client.Close()
	}
}
