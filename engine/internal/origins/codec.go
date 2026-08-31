package origins

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
)

var rawBase64 = base64.RawStdEncoding

func EncodeB64Int(value, width int) ([]byte, error) {
	if value < 0 || width < 1 {
		return nil, errors.New("valor ou largura inválida")
	}
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = byte(0x40 + (value & 0x3f))
		value >>= 6
	}
	if value != 0 {
		return nil, errors.New("valor não cabe na largura")
	}
	return out, nil
}

func DecodeB64Int(input []byte) (int, error) {
	if len(input) == 0 {
		return 0, errors.New("inteiro vazio")
	}
	value := 0
	for _, b := range input {
		digit := int(b) - 0x40
		if digit < 0 || digit > 63 {
			return 0, fmt.Errorf("byte B64 inválido: %d", b)
		}
		value = value<<6 | digit
	}
	return value, nil
}

func EncodeVL64(value int) []byte {
	negative := value < 0
	if negative {
		value = -value
	}
	bytesNeeded := 1
	for shifted := value >> 2; shifted > 0; shifted >>= 6 {
		bytesNeeded++
	}
	out := make([]byte, bytesNeeded)
	out[0] = byte(0x40 | (bytesNeeded << 3) | (value & 3))
	if negative {
		out[0] |= 4
	}
	value >>= 2
	for i := 1; i < bytesNeeded; i++ {
		out[i] = byte(0x40 | (value & 0x3f))
		value >>= 6
	}
	return out
}

func DecodeVL64(input []byte) (value, consumed int, err error) {
	if len(input) == 0 {
		return 0, 0, errors.New("inteiro VL64 vazio")
	}
	first := int(input[0]) - 0x40
	bytesNeeded := (first >> 3) & 7
	if bytesNeeded < 1 || bytesNeeded > len(input) {
		return 0, 0, errors.New("inteiro VL64 truncado")
	}
	value = first & 3
	shift := 2
	for i := 1; i < bytesNeeded; i++ {
		digit := int(input[i]) - 0x40
		if digit < 0 || digit > 63 {
			return 0, 0, errors.New("byte VL64 inválido")
		}
		value |= digit << shift
		shift += 6
	}
	if first&4 != 0 {
		value = -value
	}
	return value, bytesNeeded, nil
}

func Packet(header int, payload ...[]byte) ([]byte, error) {
	h, err := EncodeB64Int(header, 2)
	if err != nil {
		return nil, err
	}
	parts := append([][]byte{h}, payload...)
	return bytes.Join(parts, nil), nil
}

func OutgoingString(value string) ([]byte, error) {
	b := []byte(value)
	length, err := EncodeB64Int(len(b), 2)
	if err != nil {
		return nil, err
	}
	return append(length, b...), nil
}

func IncomingString(packet []byte, offset int) (string, error) {
	if offset > len(packet) {
		return "", errors.New("offset inválido")
	}
	end := bytes.IndexByte(packet[offset:], 2)
	if end < 0 {
		end = len(packet) - offset
	}
	return string(packet[offset : offset+end]), nil
}

func ClientFrame(packet []byte) ([]byte, error) {
	length, err := EncodeB64Int(len(packet), 3)
	if err != nil {
		return nil, err
	}
	return append(length, packet...), nil
}

type PlainServerBuffer struct{ data []byte }

func (b *PlainServerBuffer) Push(chunk []byte) { b.data = append(b.data, chunk...) }
func (b *PlainServerBuffer) Next() ([]byte, bool) {
	end := bytes.IndexByte(b.data, 1)
	if end < 0 {
		return nil, false
	}
	packet := append([]byte(nil), b.data[:end]...)
	b.data = b.data[end+1:]
	if len(packet) == 0 {
		return b.Next()
	}
	return packet, true
}

func Header(packet []byte) (int, error) {
	if len(packet) < 2 {
		return 0, errors.New("pacote sem cabeçalho")
	}
	return DecodeB64Int(packet[:2])
}

func EncodeBase64Bytes(input []byte) []byte {
	out := make([]byte, rawBase64.EncodedLen(len(input)))
	rawBase64.Encode(out, input)
	return out
}

func DecodeBase64Bytes(input []byte) ([]byte, error) {
	out := make([]byte, rawBase64.DecodedLen(len(input)))
	n, err := rawBase64.Decode(out, input)
	return out[:n], err
}
