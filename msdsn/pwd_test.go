package msdsn

import "testing"

// TestPwdInConnStrSimple is a simple test for pwd as a password alias
func TestPwdInConnStrSimple(t *testing.T) {
    // Test that pwd gets mapped to password in the adoSynonyms map
    if adoSynonyms["pwd"] != Password {
        t.Errorf("Expected adoSynonyms[\"pwd\"] to be %q, got %q", Password, adoSynonyms["pwd"])
    }
}
