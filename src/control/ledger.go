package control

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func Append(e Elastic, run, event, hash string, detail any, extra map[string]any) error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	doc := map[string]any{"@timestamp": Now(), "run_id": run, "event": event, "contract_hash": hash, "actor": "controller", "detail": detail}
	for k, v := range extra {
		doc[k] = v
	}
	if err := e.Do("PUT", "/sloproof-ledger/_create/"+hex.EncodeToString(b)+"?refresh=wait_for", doc, nil); err != nil {
		return fmt.Errorf("durable ledger append: %w", err)
	}
	return nil
}
