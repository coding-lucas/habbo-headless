package origins

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/chacha20"
)

var (
	bobbaG, _ = new(big.Int).SetString("23786635532332886537261431906453031264918297", 10)
	bobbaP, _ = new(big.Int).SetString("632158881801130885249042417232212770524741295422564233061391190031954228421232913648184592218883487397503624904102572293826728806813079", 10)
)

type Crypto struct {
	private, public                        *big.Int
	C2SData, C2SHeader, S2CData, S2CHeader *Stream
}
type Stream struct {
	key, nonce []byte
	counter    uint64
}

func NewCrypto() (*Crypto, error) {
	limit := new(big.Int).Sub(bobbaP, big.NewInt(2))
	private, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	private.Add(private, big.NewInt(1))
	return &Crypto{private: private, public: new(big.Int).Exp(bobbaG, private, bobbaP)}, nil
}

func (c *Crypto) PublicKey() string { return c.public.String() }

func (c *Crypto) SetPeer(value string) error {
	peer, ok := new(big.Int).SetString(value, 10)
	if !ok || peer.Sign() <= 0 || peer.Cmp(bobbaP) >= 0 {
		return errors.New("chave pública do servidor inválida")
	}
	shared := new(big.Int).Exp(peer, c.private, bobbaP).Bytes()
	var err error
	if c.C2SData, err = deriveStream(shared, "bobba-c2s-data"); err != nil {
		return err
	}
	if c.C2SHeader, err = deriveStream(shared, "bobba-c2s-header"); err != nil {
		return err
	}
	if c.S2CData, err = deriveStream(shared, "bobba-s2c-data"); err != nil {
		return err
	}
	c.S2CHeader, err = deriveStream(shared, "bobba-s2c-header")
	return err
}

func deriveStream(shared []byte, label string) (*Stream, error) {
	material, err := hkdf.Key(sha256.New, shared, []byte("BobbaXtraHKDFSalt"), "BobbaXtra|"+label, 44)
	if err != nil {
		return nil, err
	}
	return &Stream{key: material[:32], nonce: material[32:]}, nil
}

func (s *Stream) XOR(input []byte) ([]byte, error) {
	nonce := append([]byte(nil), s.nonce...)
	base := uint64(nonce[4]) | uint64(nonce[5])<<8 | uint64(nonce[6])<<16 | uint64(nonce[7])<<24 | uint64(nonce[8])<<32 | uint64(nonce[9])<<40 | uint64(nonce[10])<<48 | uint64(nonce[11])<<56
	value := base + s.counter
	s.counter++
	for i := 0; i < 8; i++ {
		nonce[4+i] = byte(value >> (8 * i))
	}
	cipher, err := chacha20.NewUnauthenticatedCipher(s.key, nonce)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(input))
	cipher.XORKeyStream(out, input)
	return out, nil
}

func (c *Crypto) EncryptClient(packet []byte) ([]byte, error) {
	payload, err := c.C2SData.XOR(packet)
	if err != nil {
		return nil, err
	}
	encodedPayload := EncodeBase64Bytes(payload)
	length, err := EncodeB64Int(len(encodedPayload), 3)
	if err != nil {
		return nil, err
	}
	header := append([]byte{42}, length...)
	encryptedHeader, err := c.C2SHeader.XOR(header)
	if err != nil {
		return nil, err
	}
	return append(EncodeBase64Bytes(encryptedHeader), encodedPayload...), nil
}

type EncryptedServerBuffer struct {
	crypto   *Crypto
	data     []byte
	expected int
}

func NewEncryptedServerBuffer(c *Crypto) *EncryptedServerBuffer {
	return &EncryptedServerBuffer{crypto: c}
}
func (b *EncryptedServerBuffer) Push(chunk []byte) { b.data = append(b.data, chunk...) }
func (b *EncryptedServerBuffer) Next() ([]byte, bool, error) {
	if b.expected == 0 {
		if len(b.data) < 6 {
			return nil, false, nil
		}
		header, err := DecodeBase64Bytes(b.data[:6])
		if err != nil {
			return nil, false, err
		}
		plain, err := b.crypto.S2CHeader.XOR(header)
		if err != nil {
			return nil, false, err
		}
		if len(plain) != 4 {
			return nil, false, fmt.Errorf("cabeçalho criptografado com %d bytes", len(plain))
		}
		b.expected, err = DecodeB64Int(plain[1:4])
		if err != nil {
			return nil, false, err
		}
	}
	if len(b.data) < 6+b.expected {
		return nil, false, nil
	}
	encoded := b.data[6 : 6+b.expected]
	ciphertext, err := DecodeBase64Bytes(encoded)
	if err != nil {
		return nil, false, err
	}
	plain, err := b.crypto.S2CData.XOR(ciphertext)
	if err != nil {
		return nil, false, err
	}
	b.data = b.data[6+b.expected:]
	b.expected = 0
	return plain, true, nil
}
