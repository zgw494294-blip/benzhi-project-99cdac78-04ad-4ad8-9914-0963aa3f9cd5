package filestore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) recoverTransactions() error {
	dir := filepath.Join(s.dir, "transactions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取事务目录: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	latest := map[string]transactionRecord{}
	seenVersion := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		var txn transactionRecord
		if err := readEnvelope(path, "transaction", &txn); err != nil {
			return s.quarantineError(path, "事务记录摘要或结构损坏", err)
		}
		if txn.Idempotency != nil && len(txn.Idempotency.Result) == 0 {
			result, marshalErr := json.Marshal(txn.Case)
			if marshalErr != nil {
				return s.quarantineError(path, "无法迁移旧版事务的幂等快照", marshalErr)
			}
			txn.Idempotency.Result = result
		}
		if err := validateTransaction(txn); err != nil {
			return s.quarantineError(path, "事务记录违反提交不变量", err)
		}
		versionKey := fmt.Sprintf("%s:%d", txn.Case.ID, txn.Case.Version)
		if seenVersion[versionKey] {
			return s.quarantineError(path, "发现重复案件版本事务", fmt.Errorf("%s", versionKey))
		}
		seenVersion[versionKey] = true
		current, ok := latest[txn.Case.ID]
		if !ok || txn.Case.Version > current.Case.Version {
			latest[txn.Case.ID] = txn
		}
		if txn.Idempotency != nil {
			prior, exists := s.recoveredIdempotency[txn.Idempotency.Key]
			if exists && prior.PayloadHash != txn.Idempotency.PayloadHash {
				return s.quarantineError(path, "事务中的幂等键载荷冲突", fmt.Errorf("key=%s", txn.Idempotency.Key))
			}
			if !exists || txn.Idempotency.CaseVersion > prior.CaseVersion {
				s.recoveredIdempotency[txn.Idempotency.Key] = *txn.Idempotency
			}
		}
	}
	for caseID, txn := range latest {
		snapshot := s.cases[caseID]
		if snapshot != nil && snapshot.Version > txn.Case.Version {
			return fmt.Errorf("案件 %s 的快照版本 %d 超过事务日志版本 %d", caseID, snapshot.Version, txn.Case.Version)
		}
		if snapshot != nil && snapshot.Version == txn.Case.Version {
			continue
		}
		path := filepath.Join(s.dir, "cases", safeName(caseID)+".json")
		if err := writeEnvelopeAtomic(path, "release-case", txn.Case); err != nil {
			return fmt.Errorf("从事务恢复案件 %s: %w", caseID, err)
		}
		copyCase, err := cloneCase(txn.Case)
		if err != nil {
			return err
		}
		s.cases[caseID] = copyCase
	}
	return nil
}

func validateTransaction(txn transactionRecord) error {
	if txn.Case == nil {
		return fmt.Errorf("缺少案件快照")
	}
	if txn.CommittedAt.IsZero() {
		return fmt.Errorf("缺少提交时间")
	}
	if txn.Expected < 0 || txn.Case.Version != txn.Expected+1 {
		return fmt.Errorf("expectedVersion=%d 与案件版本 %d 不连续", txn.Expected, txn.Case.Version)
	}
	if err := txn.Case.ValidateSnapshot(); err != nil {
		return err
	}
	if txn.Idempotency == nil {
		return fmt.Errorf("写事务缺少幂等记录")
	}
	if txn.Idempotency.Key == "" || txn.Idempotency.PayloadHash == "" || txn.Idempotency.CaseID != txn.Case.ID || txn.Idempotency.CaseVersion != txn.Case.Version || len(txn.Idempotency.Result) == 0 {
		return fmt.Errorf("幂等记录与案件版本不一致")
	}
	return nil
}

func (s *Store) quarantineError(path, reason string, cause error) error {
	name := filepath.Base(path)
	target := filepath.Join(s.dir, "quarantine", name+".corrupt")
	if _, err := os.Stat(target); err == nil {
		target = filepath.Join(s.dir, "quarantine", name+"."+safeName(strings.ReplaceAll(reason, " ", "_"))+".corrupt")
	}
	if err := os.Rename(path, target); err != nil {
		return fmt.Errorf("%s：%v；隔离 %s 失败: %w", reason, cause, path, err)
	}
	return fmt.Errorf("%s：%v；已将 %s 隔离到 %s，请修复或恢复后重新启动", reason, cause, path, target)
}
