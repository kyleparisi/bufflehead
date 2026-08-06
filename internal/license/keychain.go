package license

import "bufflehead/internal/models"

// storedInternalKey reads a coworker's internal key from the OS credential
// store. A missing entry, or a machine with no usable keychain (headless Linux
// CI), yields "" — the caller treats that as "no internal credential" and the
// detection chain moves on.
func storedInternalKey() string {
	v, err := models.GetSecret(internalKeychainLabel)
	if err != nil {
		return ""
	}
	return v
}

// StoreInternalKey saves a coworker's internal key to the OS credential store
// so they do not have to keep it in their environment. An empty key clears it.
func StoreInternalKey(key string) error {
	return models.SetSecret(internalKeychainLabel, key)
}
