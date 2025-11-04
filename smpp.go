package smpp

import (
	"context"
	"fmt"
	"time"

	"github.com/linxGnu/gosmpp"
	"github.com/linxGnu/gosmpp/data"
	"github.com/linxGnu/gosmpp/pdu"
	"go.k6.io/k6/js/modules"
)

func init() {
	modules.Register("k6/x/smpp", new(RootModule))
}

type RootModule struct{}

type ModuleInstance struct {
	vu modules.VU
}

func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	return &ModuleInstance{vu: vu}
}

func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{Default: mi}
}

type Client struct {
	conn     *gosmpp.Session
	systemID string
}

type ConnectOptions struct {
	Host            string
	Port            int
	SystemID        string
	Password        string
	SystemType      string
	InterfaceVersion uint8
	AddrTon         uint8
	AddrNpi         uint8
	AddressRange    string
	WindowSize      uint8
	ReadTimeout     int // seconds
	WriteTimeout    int // seconds
}

// Connect establishes a connection to SMPP server
func (mi *ModuleInstance) Connect(opts ConnectOptions) (*Client, error) {
	if opts.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if opts.Port == 0 {
		opts.Port = 2775
	}
	if opts.SystemID == "" {
		return nil, fmt.Errorf("systemID is required")
	}
	if opts.InterfaceVersion == 0 {
		opts.InterfaceVersion = 0x34
	}
	if opts.WindowSize == 0 {
		opts.WindowSize = 10
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = 10
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = 10
	}

	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)

	auth := gosmpp.Auth{
		SMSC:       addr,
		SystemID:   opts.SystemID,
		Password:   opts.Password,
		SystemType: opts.SystemType,
	}

	session, err := gosmpp.NewSession(
		gosmpp.TRXConnector(gosmpp.NonTLSDialer, auth),
		gosmpp.Settings{
			ReadTimeout:  time.Duration(opts.ReadTimeout) * time.Second,
			WriteTimeout: time.Duration(opts.WriteTimeout) * time.Second,
			OnPDU: func(p pdu.PDU, dir gosmpp.Direction) {
				// Optional: handle PDU events
			},
			OnReceivingError: func(err error) {
				// Handle receiving errors
			},
			OnRebindingError: func(err error) {
				// Handle rebinding errors
			},
			OnClosed: func(state gosmpp.State) {
				// Handle connection closed
			},
		},
		opts.WindowSize,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &Client{
		conn:     session,
		systemID: opts.SystemID,
	}, nil
}

type SubmitSMOptions struct {
	SourceAddr      string
	DestAddr        string
	ShortMessage    string
	SourceAddrTon   uint8
	SourceAddrNpi   uint8
	DestAddrTon     uint8
	DestAddrNpi     uint8
	ESMClass        uint8
	ProtocolID      uint8
	PriorityFlag    uint8
	RegisteredDelivery uint8
	DataCoding      uint8
	ValidityPeriod  string
	ScheduleDeliveryTime string
}

// SubmitSM sends a short message
func (c *Client) SubmitSM(opts SubmitSMOptions) (string, error) {
	if c.conn == nil {
		return "", fmt.Errorf("connection is nil")
	}

	if opts.SourceAddr == "" {
		return "", fmt.Errorf("source address is required")
	}
	if opts.DestAddr == "" {
		return "", fmt.Errorf("destination address is required")
	}

	// Set defaults
	if opts.SourceAddrTon == 0 {
		opts.SourceAddrTon = 1 // International
	}
	if opts.SourceAddrNpi == 0 {
		opts.SourceAddrNpi = 1 // E164
	}
	if opts.DestAddrTon == 0 {
		opts.DestAddrTon = 1
	}
	if opts.DestAddrNpi == 0 {
		opts.DestAddrNpi = 1
	}

	submitSM := pdu.NewSubmitSM().(*pdu.SubmitSM)
	submitSM.SourceAddr = data.SMSCAddress{Addr: opts.SourceAddr, Ton: opts.SourceAddrTon, Npi: opts.SourceAddrNpi}
	submitSM.DestAddr = data.SMSCAddress{Addr: opts.DestAddr, Ton: opts.DestAddrTon, Npi: opts.DestAddrNpi}
	submitSM.ShortMessage = opts.ShortMessage
	submitSM.ESMClass = opts.ESMClass
	submitSM.ProtocolID = opts.ProtocolID
	submitSM.PriorityFlag = opts.PriorityFlag
	submitSM.RegisteredDelivery = opts.RegisteredDelivery
	submitSM.DataCoding = opts.DataCoding
	submitSM.ValidityPeriod = opts.ValidityPeriod
	submitSM.ScheduleDeliveryTime = opts.ScheduleDeliveryTime

	resp, err := c.conn.Transceiver().Submit(submitSM)
	if err != nil {
		return "", fmt.Errorf("failed to submit message: %w", err)
	}

	return resp.MessageID, nil
}

type QuerySMOptions struct {
	MessageID  string
	SourceAddr string
	SourceAddrTon uint8
	SourceAddrNpi uint8
}

// QuerySM queries the status of a previously submitted message
func (c *Client) QuerySM(opts QuerySMOptions) (map[string]interface{}, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("connection is nil")
	}

	if opts.MessageID == "" {
		return nil, fmt.Errorf("message ID is required")
	}
	if opts.SourceAddr == "" {
		return nil, fmt.Errorf("source address is required")
	}

	if opts.SourceAddrTon == 0 {
		opts.SourceAddrTon = 1
	}
	if opts.SourceAddrNpi == 0 {
		opts.SourceAddrNpi = 1
	}

	querySM := pdu.NewQuerySM().(*pdu.QuerySM)
	querySM.MessageID = opts.MessageID
	querySM.SourceAddr = data.SMSCAddress{
		Addr: opts.SourceAddr,
		Ton:  opts.SourceAddrTon,
		Npi:  opts.SourceAddrNpi,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.conn.Transceiver().QuerySM(ctx, querySM)
	if err != nil {
		return nil, fmt.Errorf("failed to query message: %w", err)
	}

	result := map[string]interface{}{
		"messageID":   resp.MessageID,
		"finalDate":   resp.FinalDate,
		"messageState": resp.MessageState,
		"errorCode":   resp.ErrorCode,
	}

	return result, nil
}

// Close closes the SMPP connection
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	c.conn.Close()
	return nil
}

// Unbind sends an unbind request
func (c *Client) Unbind() error {
	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return c.conn.Transceiver().Unbind(ctx)
}