package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// ProviderConf is a user's configuration for one provider preset (no key).
type ProviderConf struct {
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

// ModelSettings is what the browser may see: never the keys, only whether one is set.
type ModelSettings struct {
	Active    string                  `json:"active"`
	Providers map[string]ProviderConf `json:"providers"`
	HasKey    map[string]bool         `json:"has_key"`
}

func (s *Store) loadModel(ctx context.Context, userID int64) (string, map[string]ProviderConf, map[string]string, error) {
	var active, providers string
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `SELECT active, providers, sealed_keys FROM model_settings WHERE user_id = ?`, userID).
		Scan(&active, &providers, &sealed)
	confs, keys := map[string]ProviderConf{}, map[string]string{}
	if errors.Is(err, sql.ErrNoRows) {
		return "", confs, keys, nil
	}
	if err != nil {
		return "", nil, nil, err
	}
	_ = json.Unmarshal([]byte(providers), &confs)
	if len(sealed) > 0 {
		plain, err := s.sealer.open(sealed, userID)
		if err != nil {
			return "", nil, nil, errors.New("stored API keys can't be decrypted; was PAYLIST_SECRET changed?")
		}
		_ = json.Unmarshal(plain, &keys)
	}
	return active, confs, keys, nil
}

func (s *Store) GetModelSettings(ctx context.Context, userID int64) (ModelSettings, error) {
	active, confs, keys, err := s.loadModel(ctx, userID)
	if err != nil {
		return ModelSettings{}, err
	}
	has := map[string]bool{}
	for k, v := range keys {
		has[k] = v != ""
	}
	return ModelSettings{Active: active, Providers: confs, HasKey: has}, nil
}

// ActiveProvider returns the user's selected provider configuration and its key.
func (s *Store) ActiveProvider(ctx context.Context, userID int64) (string, ProviderConf, string, error) {
	active, confs, keys, err := s.loadModel(ctx, userID)
	if err != nil {
		return "", ProviderConf{}, "", err
	}
	if active == "" {
		return "", ProviderConf{}, "", ErrNotFound
	}
	return active, confs[active], keys[active], nil
}

// SaveProvider stores a preset's configuration and makes it the active one.
// key == nil keeps the stored key; a pointer to "" removes it.
func (s *Store) SaveProvider(ctx context.Context, userID int64, preset string, conf ProviderConf, key *string) (ModelSettings, error) {
	preset = strings.TrimSpace(preset)
	if preset == "" || len(preset) > 40 {
		return ModelSettings{}, errors.New("invalid provider")
	}
	_, confs, keys, err := s.loadModel(ctx, userID)
	if err != nil {
		return ModelSettings{}, err
	}
	confs[preset] = conf
	if key != nil {
		if k := strings.TrimSpace(*key); k == "" {
			delete(keys, preset)
		} else {
			keys[preset] = k
		}
	}
	pj, _ := json.Marshal(confs)
	kj, _ := json.Marshal(keys)
	_, err = s.db.ExecContext(ctx, `INSERT INTO model_settings (user_id, active, providers, sealed_keys) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET active = excluded.active, providers = excluded.providers, sealed_keys = excluded.sealed_keys`,
		userID, preset, string(pj), s.sealer.seal(kj, userID))
	if err != nil {
		return ModelSettings{}, err
	}
	return s.GetModelSettings(ctx, userID)
}
