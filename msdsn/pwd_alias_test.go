package msdsn

import "testing"

// TestPwdAlias verifies that "pwd" is correctly recognized as an alias for "password"
func TestPwdAlias(t *testing.T) {
	// Test that pwd gets mapped to password in the adoSynonyms map
	if adoSynonyms["pwd"] != Password {
		t.Errorf("Expected adoSynonyms[\"pwd\"] to be %q, got %q", Password, adoSynonyms["pwd"])
	}
}