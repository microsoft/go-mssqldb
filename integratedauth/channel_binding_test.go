package integratedauth

import (
	"encoding/hex"
	"testing"
)

func TestGenerateCBTFromTLSUnique(t *testing.T) {
	pairs := map[string]string{
		"0123456789abcdef": "1eb7620a5e38cb1f50478b1690621a03",
	}
	for input, expected := range pairs {
		channelBinding, err := GenerateCBTFromTLSUnique([]byte(input))
		if err != nil {
			t.Errorf("Expected no error, but got %v for input %s", err, input)
		}
		actual := channelBinding.Md5Hash()
		if hex.EncodeToString(actual) != expected {
			t.Errorf("Expected %s, but got %s for input %s", expected, hex.EncodeToString(actual), input)
		}
	}
}

func TestAsSSPI_SEC_CHANNEL_BINDINGS(t *testing.T) {
	pairs := map[string]string{
		"0123456789abcdef": "0000000000000000200000000000000000000000200000001b00000020000000746c732d756e697175653a30313233343536373839616263646566",
	}
	for input, expected := range pairs {
		channelBinding, err := GenerateCBTFromTLSUnique([]byte(input))
		if err != nil {
			t.Errorf("Expected no error, but got %v for input %s", err, input)
		}
		winsspiCB := channelBinding.AsSSPI_SEC_CHANNEL_BINDINGS().ToBytes()
		if hex.EncodeToString(winsspiCB) != expected {
			t.Errorf("Expected %s, but got %s for input %s", expected, hex.EncodeToString(winsspiCB), input)
		}
	}
}
