package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	defaultServerProgName = "GO MSSQL Server"
	defaultServerVerion   = "v16.0.0"
)

type Client struct {
	Conn *Conn
}

type Server struct {
	ConnTimeout time.Duration
	PacketSize  uint16
	Logger      ContextLogger
	Version     uint32
	ProgName    string
	Encryption  byte
}

type ServerConfig struct {
	ConnTimeout *time.Duration
	PacketSize  *uint16
	Logger      ContextLogger
	Version     *string
	Encryption  *string
	ProgName    *string
}

func NewServer(config ServerConfig) (*Server, error) {
	server := &Server{}

	if config.PacketSize == nil {
		server.PacketSize = defaultPacketSize
	} else {
		server.PacketSize = *config.PacketSize
	}
	// Ensure packet size falls within the TDS protocol range of 512 to 32767 bytes
	// NOTE: Encrypted connections have a maximum size of 16383 bytes.  If you request
	// a higher packet size, the server will respond with an ENVCHANGE request to
	// alter the packet size to 16383 bytes.
	if server.PacketSize < 512 {
		server.PacketSize = 512
	} else if server.PacketSize > 32767 {
		server.PacketSize = 32767
	}

	if config.ConnTimeout != nil {
		server.ConnTimeout = *config.ConnTimeout
	}

	if config.Logger != nil {
		server.Logger = config.Logger
	}

	if config.Version != nil {
		server.Version = getDriverVersion(*config.Version)
	} else {
		server.Version = getDriverVersion(defaultServerVerion)
	}

	if config.ProgName != nil {
		server.ProgName = *config.ProgName
	} else {
		server.ProgName = defaultServerProgName
	}

	if config.Encryption != nil {
		switch *config.Encryption {
		case "strict":
			server.Encryption = encryptStrict
		case "required":
			server.Encryption = encryptReq
		case "on":
			server.Encryption = encryptOn
		case "off":
			server.Encryption = encryptOff
		default:
			return nil, errors.New("invalid encryption option")
		}
	} else {
		server.Encryption = encryptNotSup
	}

	return server, nil
}

func (s *Server) NewTdsServerSession(conn net.Conn) (*tdsSession, *login, error) {
	toconn := newTimeoutConn(conn, s.ConnTimeout)
	inbuf := newTdsBuffer(s.PacketSize, toconn)

	login, err := s.handshake(inbuf)
	if err != nil {
		return nil, nil, err
	}

	sess := tdsSession{
		buf:    inbuf,
		logger: s.Logger,
	}

	return &sess, &login, nil
}

func (s *tdsSession) ReadCommand() (packetType, error) {
	for {
		_, err := s.buf.BeginRead()
		if err != nil {
			return 0, err
		}

		if s.buf.final {
			return s.buf.rPacketType, nil
		}
	}
}

func (s *Server) handshake(r *tdsBuffer) (login, error) {
	var login login

	err := s.readPrelogin(r)
	if err != nil {
		return login, err
	}

	err = s.writePrelogin(r)
	if err != nil {
		return login, err
	}

	login, err = s.readLogin(r)
	if err != nil {
		return login, err
	}

	err = s.writeLogin(r)
	if err != nil {
		return login, err
	}

	return login, nil
}

func (s *Server) readPrelogin(r *tdsBuffer) error {
	packet_type, err := r.BeginRead()
	if err != nil {
		return err
	}
	struct_buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if packet_type != packPrelogin {
		return errors.New("invalid request, expected pre-login packet")
	}
	if len(struct_buf) == 0 {
		return errors.New("invalid empty PRELOGIN request, it must contain at least one byte")
	}

	offset := 0
	results := map[uint8][]byte{}
	for {
		// read prelogin option
		plOption, err := readPreloginOption(struct_buf, offset)
		if err != nil {
			return err
		}

		if plOption.token == preloginTERMINATOR {
			break
		}

		// read prelogin option data
		value, err := readPreloginOptionData(plOption, struct_buf)
		if err != nil {
			return err
		}
		results[plOption.token] = value

		offset += preloginOptionSize
	}

	return nil
}

func (s *Server) writePrelogin(r *tdsBuffer) error {
	if err := writePrelogin(packReply, r, s.preparePreloginResponseFields()); err != nil {
		return err
	}

	return nil
}

func (s *Server) preparePreloginResponseFields() map[uint8][]byte {
	fields := map[uint8][]byte{
		// 4 bytes for version and 2 bytes for minor version
		preloginVERSION:    {byte(s.Version), byte(s.Version >> 8), byte(s.Version >> 16), byte(s.Version >> 24), 0, 0},
		preloginENCRYPTION: {s.Encryption},
		preloginINSTOPT:    {0},
		preloginTHREADID:   {0, 0, 0, 0},
		preloginMARS:       {0}, // MARS disabled
	}

	return fields
}

func (s *Server) readLogin(r *tdsBuffer) (login, error) {
	var login login

	packet_type, err := r.BeginRead()
	if err != nil {
		return login, err
	}

	if packet_type != packLogin7 {
		return login, errors.New("invalid request, expected login packet")
	}

	struct_buf, err := io.ReadAll(r)
	if err != nil {
		return login, err
	}

	if len(struct_buf) == 0 {
		return login, errors.New("invalid empty login request, it must contain at least one byte")
	}

	var loginHeader loginHeader
	if err := binary.Read(bytes.NewReader(struct_buf), binary.LittleEndian, &loginHeader); err != nil {
		return login, fmt.Errorf("failed to read login packet: %w", err)
	}

	login.TDSVersion = loginHeader.TDSVersion
	login.ClientPID = loginHeader.ClientPID
	login.ConnectionID = loginHeader.ConnectionID
	login.OptionFlags1 = loginHeader.OptionFlags1
	login.OptionFlags2 = loginHeader.OptionFlags2
	login.TypeFlags = loginHeader.TypeFlags
	login.OptionFlags3 = loginHeader.OptionFlags3
	login.ClientTimeZone = loginHeader.ClientTimeZone
	login.ClientLCID = loginHeader.ClientLCID
	login.ClientID = loginHeader.ClientID

	login.HostName, err = readLoginFieldString(struct_buf, loginHeader.HostNameOffset, loginHeader.HostNameLength)
	if err != nil {
		return login, fmt.Errorf("failed to read hostname: %w", err)
	}
	login.UserName, err = readLoginFieldString(struct_buf, loginHeader.UserNameOffset, loginHeader.UserNameLength)
	if err != nil {
		return login, fmt.Errorf("failed to read username: %w", err)
	}
	login.AppName, err = readLoginFieldString(struct_buf, loginHeader.AppNameOffset, loginHeader.AppNameLength)
	if err != nil {
		return login, fmt.Errorf("failed to read username: %w", err)
	}
	login.ServerName, err = readLoginFieldString(struct_buf, loginHeader.ServerNameOffset, loginHeader.ServerNameLength)
	if err != nil {
		return login, fmt.Errorf("failed to read servername: %w", err)
	}
	login.CtlIntName, err = readLoginFieldString(struct_buf, loginHeader.CtlIntNameOffset, loginHeader.CtlIntNameLength)
	if err != nil {
		return login, fmt.Errorf("failed to read servername: %w", err)
	}
	login.Language, err = readLoginFieldString(struct_buf, loginHeader.LanguageOffset, loginHeader.LanguageLength)
	if err != nil {
		return login, fmt.Errorf("failed to read servername: %w", err)
	}
	login.Database, err = readLoginFieldString(struct_buf, loginHeader.DatabaseOffset, loginHeader.DatabaseLength)
	if err != nil {
		return login, fmt.Errorf("failed to read servername: %w", err)
	}
	login.SSPI, err = readLoginFieldBytes(struct_buf, loginHeader.SSPIOffset, loginHeader.SSPILength)
	if err != nil {
		return login, fmt.Errorf("failed to read sspi: %w", err)
	}
	login.AtchDBFile, err = readLoginFieldString(struct_buf, loginHeader.AtchDBFileOffset, loginHeader.AtchDBFileLength)
	if err != nil {
		return login, fmt.Errorf("failed to read sspi: %w", err)
	}
	login.ChangePassword, err = readLoginFieldString(struct_buf, loginHeader.ChangePasswordOffset, loginHeader.ChangePasswordLength)
	if err != nil {
		return login, fmt.Errorf("failed to read sspi: %w", err)
	}

	return login, nil
}

func readLoginFieldString(b []byte, offset uint16, length uint16) (string, error) {
	if len(b) < int(offset)+int(length)*2 {
		return "", fmt.Errorf("invalid login packet, expected %d bytes, got %d", offset+length*2, len(b))
	}

	return ucs22str(b[offset : offset+length*2])
}

func readLoginFieldBytes(b []byte, offset uint16, length uint16) ([]byte, error) {
	if len(b) < int(offset)+int(length)*2 {
		return nil, fmt.Errorf("invalid login packet, expected %d bytes, got %d", offset+length*2, len(b))
	}

	return b[offset : offset+length*2], nil
}

func (s *Server) writeLogin(r *tdsBuffer) error {
	loginAckStruct := loginAckStruct{
		Interface:  1,
		TDSVersion: verTDS74,
		ProgName:   s.ProgName,
		ProgVer:    s.Version,
	}

	doneStruct := doneStruct{
		Status:   0,
		CurCmd:   0,
		RowCount: 0,
		errors:   []Error{},
	}

	r.BeginPacket(packReply, false)
	r.Write(writeLoginAck(loginAckStruct))
	r.Write(writeDone(doneStruct))

	return r.FinishPacket()
}

func UCS2String(s []byte) (string, error) {
	return ucs22str(s)
}

func (c *Conn) Transport() io.ReadWriteCloser {
	if c.sess == nil || c.sess.buf == nil {
		return nil
	}

	return c.sess.buf.transport
}

func (c *Conn) Buffer() *tdsBuffer {
	if c.sess == nil || c.sess.buf == nil {
		return nil
	}

	return c.sess.buf
}

func (c *Conn) Session() *tdsSession {
	return c.sess
}

func (s *tdsSession) ParseHeader() (header, error) {
	var h header
	err := binary.Read(s.buf, binary.LittleEndian, &h)
	if err != nil {
		return header{}, err
	}

	return h, nil
}

func (s *tdsSession) ParseSQLBatch() ([]headerStruct, string, error) {
	headers, err := readAllHeaders(s.buf)
	if err != nil {
		return nil, "", err
	}

	query, err := readUcs2(s.buf, (s.buf.rsize-s.buf.rpos)/2)
	if err != nil {
		return nil, "", err
	}

	return headers, query, nil
}

func (s *tdsSession) ParseRPC() ([]headerStruct, procId, uint16, []param, []interface{}, error) {
	headers, err := readAllHeaders(s.buf)
	if err != nil {
		return nil, procId{}, 0, nil, nil, err
	}

	var nameLength uint16
	if err := binary.Read(s.buf, binary.LittleEndian, &nameLength); err != nil {
		return nil, procId{}, 0, nil, nil, err
	}

	var proc procId
	var idswitch uint16 = 0xffff
	if nameLength == idswitch {
		if err := binary.Read(s.buf, binary.LittleEndian, &proc.id); err != nil {
			return nil, procId{}, 0, nil, nil, err
		}
	} else {
		proc.name, err = readUcs2(s.buf, int(nameLength))
		if err != nil {
			return nil, procId{}, 0, nil, nil, err
		}
	}

	var flags uint16
	if err := binary.Read(s.buf, binary.LittleEndian, &flags); err != nil {
		return nil, procId{}, 0, nil, nil, err
	}

	params, values, err := parseParams(s.buf)
	if err != nil {
		return nil, procId{}, 0, nil, nil, err
	}

	return headers, proc, flags, params, values, nil
}

func parseParams(b *tdsBuffer) ([]param, []interface{}, error) {
	var params []param
	var values []interface{}
	for {
		if b.rpos >= b.rsize {
			break
		}

		var p param

		name, err := readBVarChar(b)
		if err != nil {
			return nil, nil, err
		}
		p.Name = name

		var flags uint8
		if err := binary.Read(b, binary.LittleEndian, &flags); err != nil {
			return nil, nil, err
		}

		p.Flags = flags

		p.ti = readTypeInfo(b, b.byte(), nil)
		val := p.ti.Reader(&p.ti, b, nil)
		p.buffer = p.ti.Buffer

		params = append(params, p)
		values = append(values, val)
	}
	return params, values, nil
}

func readAllHeaders(r io.Reader) ([]headerStruct, error) {
	var totalLength uint32
	err := binary.Read(r, binary.LittleEndian, &totalLength)
	if err != nil {
		return nil, err
	}

	if totalLength < 4 {
		return nil, errors.New("invalid total length")
	}

	var headers []headerStruct
	remainingLength := totalLength - 4 // Subtracting the length of the totalLength field

	for remainingLength > 0 {
		var headerLength uint32
		err = binary.Read(r, binary.LittleEndian, &headerLength)
		if err != nil {
			return nil, err
		}

		if headerLength < 6 || headerLength-6 > remainingLength {
			return nil, errors.New("invalid header length")
		}

		var hdrtype uint16
		err = binary.Read(r, binary.LittleEndian, &hdrtype)
		if err != nil {
			return nil, err
		}

		dataLength := headerLength - 6 // Subtracting the length of the headerLength and hdrtype fields
		data := make([]byte, dataLength)
		_, err = io.ReadFull(r, data)
		if err != nil {
			return nil, err
		}

		headers = append(headers, headerStruct{
			hdrtype: hdrtype,
			data:    data,
		})

		remainingLength -= headerLength
	}

	if remainingLength != 0 {
		return nil, errors.New("inconsistent header length")
	}

	return headers, nil
}

func (p *procId) Id() uint16 {
	return p.id
}

func (p *procId) Name() string {
	return p.name
}

func writeDone(d doneStruct) []byte {
	data := make([]byte, 0, 12)

	// Append tokenDone and the calculated size
	data = append(data, byte(tokenDone))

	// Append Status
	statusBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(statusBytes, d.Status)
	data = append(data, statusBytes...)

	// Append CurCmd
	curCmdBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(curCmdBytes, d.CurCmd)
	data = append(data, curCmdBytes...)

	// Append RowCount
	rowCountBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(rowCountBytes, d.RowCount)
	data = append(data, rowCountBytes...)

	return data
}

func writeLoginAck(l loginAckStruct) []byte {
	progNameUCS2 := str2ucs2(l.ProgName)

	// Prepare the slice with preallocated size for efficiency
	data := make([]byte, 0, 10+len(progNameUCS2))

	// Append tokenLoginAck
	data = append(data, byte(tokenLoginAck))

	// Append calculated size
	size := uint16(10 + len(progNameUCS2))
	sizeBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(sizeBytes, size)
	data = append(data, sizeBytes...)

	// Append Interface
	data = append(data, l.Interface)

	// Append TDSVersion
	tdsVersionBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(tdsVersionBytes, l.TDSVersion)
	data = append(data, tdsVersionBytes...)

	// Append ProgName Length and ProgName
	data = append(data, byte(len(progNameUCS2)/2))
	data = append(data, progNameUCS2...)

	// Append ProgVer
	progVerBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(progVerBytes, l.ProgVer)
	data = append(data, progVerBytes...)

	return data
}

func NewClient(ctx context.Context, dsn string) (*Client, error) {
	c, err := NewConnector(dsn)
	if err != nil {
		return nil, err
	}

	conn, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}

	mssqlConn, ok := conn.(*Conn)
	if !ok {
		return nil, fmt.Errorf("invalid conn")
	}

	return &Client{
		Conn: mssqlConn,
	}, nil
}

func (c *Client) Close() error {
	return c.Conn.Close()
}

func (c *Client) SendSqlBatch(ctx context.Context, serverConn *tdsSession, query string, headers []headerStruct, resetSession bool) ([]doneStruct, error) {
	if err := sendSqlBatch72(c.Conn.sess.buf, query, headers, resetSession); err != nil {
		return nil, err
	}

	return c.processResponse(ctx, serverConn)
}

func (c *Client) SendRpc(ctx context.Context, serverConn *tdsSession, headers []headerStruct, proc procId, flags uint16, params []param, resetSession bool) ([]doneStruct, error) {
	if err := sendRpc(c.Conn.sess.buf, headers, proc, flags, params, resetSession); err != nil {
		return nil, err
	}

	return c.processResponse(ctx, serverConn)
}

func (c *Client) processResponse(ctx context.Context, sess *tdsSession) ([]doneStruct, error) {
	c.Conn.sess.buf.serverConn = sess

	packet_type, err := c.Conn.sess.buf.BeginRead()
	if err != nil {
		switch e := err.(type) {
		case *net.OpError:
			return nil, e
		default:
			return nil, &net.OpError{Op: "Read", Err: err}
		}
	}

	if packet_type != packReply {
		return nil, StreamError{
			InnerError: fmt.Errorf("unexpected packet type in reply: got %v, expected %v", packet_type, packReply),
		}
	}

	var dones []doneStruct
	var columns []columnStruct
	var errs []Error
	for {
		token := token(c.Conn.sess.buf.byte())
		switch token {
		case tokenReturnStatus:
			parseReturnStatus(c.Conn.sess.buf)
		case tokenOrder:
			parseOrder(c.Conn.sess.buf)
		case tokenDone, tokenDoneProc, tokenDoneInProc:
			res := parseDone(c.Conn.sess.buf)
			res.errors = errs
			dones = append(dones, res)
			if res.Status&doneSrvError != 0 {
				return dones, ServerError{res.getError()}
			}

			if res.Status&doneMore == 0 {
				return dones, nil
			}
		case tokenColMetadata:
			columns = parseColMetadata72(c.Conn.sess.buf, sess)
		case tokenRow:
			row := make([]interface{}, len(columns))
			err = parseRow(ctx, c.Conn.sess.buf, c.Conn.sess, columns, row)
			if err != nil {
				return nil, StreamError{
					InnerError: fmt.Errorf("failed to parse row: %w", err),
				}
			}
		case tokenNbcRow:
			row := make([]interface{}, len(columns))
			err = parseNbcRow(ctx, c.Conn.sess.buf, c.Conn.sess, columns, row)
			if err != nil {
				return nil, StreamError{
					InnerError: fmt.Errorf("failed to parse row: %w", err),
				}
			}
		case tokenEnvChange:
			processEnvChg(ctx, c.Conn.sess)
		case tokenError:
			err := parseError72(c.Conn.sess.buf)
			errs = append(errs, err)
		case tokenInfo:
			info := parseInfo(c.Conn.sess.buf)
			fmt.Printf("got INFO %d %s\n", info.Number, info.Message)
		case tokenReturnValue:
			parseReturnValue(c.Conn.sess.buf, c.Conn.sess)
		default:
			return nil, StreamError{
				InnerError: fmt.Errorf("unknown token type returned: %v", token),
			}
		}
	}
}
