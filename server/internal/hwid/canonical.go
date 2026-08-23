package hwid

import (
	"bytes"
	"encoding/binary"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
)

const canonicalDomain = "laminara.machine.v1\x00"

func Canonical(report *apiv1.MachineReport) []byte {
	var buf bytes.Buffer
	buf.WriteString(canonicalDomain)
	writeU32(&buf, report.SchemaVersion)

	writeU32(&buf, uint32(len(report.Signals)))
	for _, signal := range report.Signals {
		writeI32(&buf, int32(signal.Kind))
		writeU32(&buf, uint32(len(signal.Digest)))
		buf.Write(signal.Digest)
	}

	writeU32(&buf, uint32(len(report.Flags)))
	for _, flag := range report.Flags {
		writeI32(&buf, int32(flag))
	}

	writeI32(&buf, int32(report.Platform))
	writeI64(&buf, report.CollectedAtUnixNanos)
	writeU32(&buf, uint32(len(report.Nonce)))
	buf.Write(report.Nonce)
	return buf.Bytes()
}

func writeU32(buf *bytes.Buffer, value uint32) {
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], value)
	buf.Write(scratch[:])
}

func writeI32(buf *bytes.Buffer, value int32) {
	writeU32(buf, uint32(value))
}

func writeI64(buf *bytes.Buffer, value int64) {
	var scratch [8]byte
	binary.LittleEndian.PutUint64(scratch[:], uint64(value))
	buf.Write(scratch[:])
}
