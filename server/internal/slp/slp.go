package slp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const maxStatusBytes = 4 << 20

type Status struct {
	Online  int64
	Max     int64
	Version string
	Sample  []string
}

const protocolVersion = 767

func SplitAddress(address string) (string, uint16) {
	address = strings.TrimSpace(address)
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address, 25565
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return address, 25565
	}
	return host, uint16(number)
}

func PingContext(ctx context.Context, address string, timeout time.Duration) (Status, error) {
	host, port := SplitAddress(address)
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		return Status{}, err
	}
	defer conn.Close()
	deadline := time.Now().Add(timeout)
	if at, ok := ctx.Deadline(); ok && at.Before(deadline) {
		deadline = at
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	handshake := []byte{0x00}
	handshake = appendVarint(handshake, protocolVersion)
	handshake = appendString(handshake, host)
	handshake = binary.BigEndian.AppendUint16(handshake, port)
	handshake = appendVarint(handshake, 1)
	if _, err := conn.Write(frame(handshake)); err != nil {
		return Status{}, err
	}
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return Status{}, err
	}

	reader := &byteReader{Reader: conn}
	if _, err := readVarint(reader); err != nil {
		return Status{}, err
	}
	if _, err := readVarint(reader); err != nil {
		return Status{}, err
	}
	length, err := readVarint(reader)
	if err != nil {
		return Status{}, err
	}
	if length <= 0 || length > maxStatusBytes {
		return Status{}, fmt.Errorf("сервер ответил пакетом на %d байт — это не статус", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return Status{}, err
	}
	return decode(payload)
}

func decode(payload []byte) (Status, error) {
	var body struct {
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
		Players struct {
			Online int64 `json:"online"`
			Max    int64 `json:"max"`
			Sample []struct {
				Name string `json:"name"`
			} `json:"sample"`
		} `json:"players"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return Status{}, err
	}
	status := Status{Online: body.Players.Online, Max: body.Players.Max, Version: body.Version.Name}
	for _, entry := range body.Players.Sample {
		if entry.Name != "" {
			status.Sample = append(status.Sample, entry.Name)
		}
	}
	return status, nil
}

func frame(payload []byte) []byte {
	return append(appendVarint(nil, uint32(len(payload))), payload...)
}

func appendVarint(buf []byte, value uint32) []byte {
	for {
		part := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			part |= 0x80
		}
		buf = append(buf, part)
		if value == 0 {
			return buf
		}
	}
}

func appendString(buf []byte, text string) []byte {
	buf = appendVarint(buf, uint32(len(text)))
	return append(buf, text...)
}

type byteReader struct {
	io.Reader
}

func (r *byteReader) ReadByte() (byte, error) {
	var one [1]byte
	if _, err := io.ReadFull(r.Reader, one[:]); err != nil {
		return 0, err
	}
	return one[0], nil
}

func readVarint(reader io.ByteReader) (uint32, error) {
	var result uint32
	var shift uint
	for {
		part, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(part&0x7f) << shift
		if part&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift >= 32 {
			return 0, io.ErrUnexpectedEOF
		}
	}
}
