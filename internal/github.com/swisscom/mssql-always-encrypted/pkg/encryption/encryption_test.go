package encryption

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestType_Plaintext(t *testing.T) {
	t.Parallel()
	assert.False(t, Plaintext.Deterministic, "Plaintext should not be deterministic")
	assert.Equal(t, "Plaintext", Plaintext.Name)
	assert.Equal(t, byte(0), Plaintext.Value)
}

func TestType_Deterministic(t *testing.T) {
	t.Parallel()
	assert.True(t, Deterministic.Deterministic, "Deterministic should be deterministic")
	assert.Equal(t, "Deterministic", Deterministic.Name)
	assert.Equal(t, byte(1), Deterministic.Value)
}

func TestType_Randomized(t *testing.T) {
	t.Parallel()
	assert.False(t, Randomized.Deterministic, "Randomized should not be deterministic")
	assert.Equal(t, "Randomized", Randomized.Name)
	assert.Equal(t, byte(2), Randomized.Value)
}

func TestFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    byte
		expected Type
	}{
		{0, Plaintext},
		{1, Deterministic},
		{2, Randomized},
		{3, Plaintext},   // unknown defaults to Plaintext
		{255, Plaintext}, // unknown defaults to Plaintext
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.expected.Name, func(t *testing.T) {
			t.Parallel()
			result := From(tc.input)
			assert.Equal(t, tc.expected, result, "From(%d) mismatch", tc.input)
		})
	}
}

func TestFrom_AllValues(t *testing.T) {
	t.Parallel()
	// Test that all valid encryption types are covered
	assert.Equal(t, Plaintext, From(0), "From(0) should return Plaintext")
	assert.Equal(t, Deterministic, From(1), "From(1) should return Deterministic")
	assert.Equal(t, Randomized, From(2), "From(2) should return Randomized")
}
