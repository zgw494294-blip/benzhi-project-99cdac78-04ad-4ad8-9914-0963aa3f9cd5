package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

const schemaVersion = 1

type envelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Kind          string          `json:"kind"`
	Digest        string          `json:"digest"`
	Data          json.RawMessage `json:"data"`
}

type Store struct {
	dir                  string
	cases                map[string]*domain.ReleaseCase
	idempotency          map[string]application.IdempotencyRecord
	recoveredIdempotency map[string]application.IdempotencyRecord
	mu                   sync.RWMutex
}

type transactionRecord struct {
	Case        *domain.ReleaseCase            `json:"case"`
	Expected    int64                          `json:"expectedVersion"`
	Idempotency *application.IdempotencyRecord `json:"idempotency,omitempty"`
	CommittedAt time.Time                      `json:"committedAt"`
}

func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("数据目录不能为空")
	}
	s := &Store{dir: dir, cases: map[string]*domain.ReleaseCase{}, idempotency: map[string]application.IdempotencyRecord{}, recoveredIdempotency: map[string]application.IdempotencyRecord{}}
	for _, sub := range []string{"cases", "transactions", "quarantine"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			return nil, fmt.Errorf("创建数据目录 %s: %w", sub, err)
		}
	}
	if err := s.loadCases(); err != nil {
		return nil, err
	}
	if err := s.recoverTransactions(); err != nil {
		return nil, err
	}
	if err := s.loadIdempotency(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Get(ctx context.Context, id string) (*domain.ReleaseCase, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cases[id]
	if c == nil {
		return nil, domain.NewError("CASE_NOT_FOUND", "开放案件不存在")
	}
	return cloneCase(c)
}

func (s *Store) GetByCredential(ctx context.Context, no string) (*domain.ReleaseCase, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.cases {
		if c.Credential != nil && c.Credential.CredentialNo == no {
			return cloneCase(c)
		}
	}
	return nil, domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在")
}

func (s *Store) Save(ctx context.Context, c *domain.ReleaseCase, expectedVersion int64, idem *application.IdempotencyRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.ID == "" {
		return fmt.Errorf("拒绝保存空案件")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.cases[c.ID]
	if expectedVersion == 0 {
		if existing != nil {
			return domain.NewError("CASE_ALREADY_EXISTS", "开放案件已存在")
		}
		if c.Version != 1 {
			return fmt.Errorf("新案件版本必须为 1")
		}
	} else if existing == nil || existing.Version != expectedVersion {
		actual := int64(0)
		if existing != nil {
			actual = existing.Version
		}
		return &domain.Error{Code: "VERSION_CONFLICT", Message: "持久化时发现案件版本冲突", Fields: map[string]string{"currentVersion": fmt.Sprint(actual)}}
	}
	if idem != nil {
		if prior, ok := s.idempotency[idem.Key]; ok && prior.PayloadHash != idem.PayloadHash {
			return domain.NewError("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求")
		}
	}
	copyCase, err := cloneCase(c)
	if err != nil {
		return err
	}
	if err := copyCase.ValidateSnapshot(); err != nil {
		return fmt.Errorf("拒绝保存违反领域不变量的案件: %w", err)
	}
	txn := transactionRecord{Case: copyCase, Expected: expectedVersion, Idempotency: idem, CommittedAt: time.Now().UTC()}
	txnPath := filepath.Join(s.dir, "transactions", fmt.Sprintf("%s-v%020d.json", safeName(c.ID), c.Version))
	if err := writeEnvelopeAtomic(txnPath, "transaction", txn); err != nil {
		return fmt.Errorf("写入事务记录: %w", err)
	}
	casePath := filepath.Join(s.dir, "cases", safeName(c.ID)+".json")
	if err := writeEnvelopeAtomic(casePath, "release-case", copyCase); err != nil {
		return fmt.Errorf("写入案件快照: %w", err)
	}
	newIndex := make(map[string]application.IdempotencyRecord, len(s.idempotency)+1)
	for k, value := range s.idempotency {
		newIndex[k] = value
	}
	if idem != nil {
		newIndex[idem.Key] = *idem
	}
	if err := writeEnvelopeAtomic(filepath.Join(s.dir, "idempotency.json"), "idempotency-index", newIndex); err != nil {
		return fmt.Errorf("写入幂等索引: %w", err)
	}
	s.cases[c.ID], s.idempotency = copyCase, newIndex
	return nil
}

func (s *Store) LookupIdempotency(ctx context.Context, key string) (*application.IdempotencyRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.idempotency[key]
	if !ok {
		return nil, domain.NewError("IDEMPOTENCY_NOT_FOUND", "幂等记录不存在")
	}
	copy := record
	return &copy, nil
}

func (s *Store) loadCases() error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "cases"))
	if err != nil {
		return fmt.Errorf("读取案件目录: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dir, "cases", entry.Name())
		var c domain.ReleaseCase
		if err := readEnvelope(path, "release-case", &c); err != nil {
			return fmt.Errorf("案件快照损坏 %s（原文件保留，可由管理员隔离后恢复）: %w", entry.Name(), err)
		}
		if c.ID == "" || c.Version < 1 {
			return fmt.Errorf("案件快照 %s 缺少必要标识或版本", entry.Name())
		}
		if err := c.ValidateSnapshot(); err != nil {
			return fmt.Errorf("案件快照 %s 违反领域不变量（原文件保留）: %w", entry.Name(), err)
		}
		if _, duplicate := s.cases[c.ID]; duplicate {
			return fmt.Errorf("发现重复案件 %s", c.ID)
		}
		copy := c
		s.cases[c.ID] = &copy
	}
	return nil
}

func (s *Store) loadIdempotency() error {
	path := filepath.Join(s.dir, "idempotency.json")
	var records map[string]application.IdempotencyRecord
	err := readEnvelope(path, "idempotency-index", &records)
	if errors.Is(err, os.ErrNotExist) {
		records = map[string]application.IdempotencyRecord{}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("幂等索引损坏（原文件保留）: %w", err)
	}
	for key, recovered := range s.recoveredIdempotency {
		if existing, ok := records[key]; ok && existing.PayloadHash != recovered.PayloadHash {
			return fmt.Errorf("恢复的幂等记录 %s 与索引冲突", key)
		}
		if current, ok := records[key]; !ok || recovered.CaseVersion > current.CaseVersion {
			records[key] = recovered
		}
	}
	for key, record := range records {
		if len(record.Result) == 0 {
			if recovered, ok := s.recoveredIdempotency[key]; ok {
				records[key] = recovered
			}
		}
	}
	for key, record := range records {
		c := s.cases[record.CaseID]
		if c == nil || record.CaseVersion > c.Version {
			return fmt.Errorf("幂等记录 %s 指向不存在的案件版本", key)
		}
		if len(record.Result) == 0 {
			return fmt.Errorf("幂等记录 %s 缺少响应快照", key)
		}
	}
	s.idempotency = records
	if len(s.recoveredIdempotency) > 0 {
		if err := writeEnvelopeAtomic(path, "idempotency-index", records); err != nil {
			return fmt.Errorf("刷新恢复后的幂等索引: %w", err)
		}
	}
	return nil
}

func writeEnvelopeAtomic(path, kind string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	wrapped, err := json.MarshalIndent(envelope{SchemaVersion: schemaVersion, Kind: kind, Digest: hex.EncodeToString(sum[:]), Data: data}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pending-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(wrapped); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	if err := d.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func readEnvelope(path, kind string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var wrapped envelope
	if err := json.Unmarshal(b, &wrapped); err != nil {
		return err
	}
	if wrapped.SchemaVersion != schemaVersion {
		return fmt.Errorf("不支持 schemaVersion %d", wrapped.SchemaVersion)
	}
	if wrapped.Kind != kind {
		return fmt.Errorf("记录类型应为 %s，实际为 %s", kind, wrapped.Kind)
	}
	var canonical bytes.Buffer
	if err := json.Compact(&canonical, wrapped.Data); err != nil {
		return fmt.Errorf("记录 data 不是有效 JSON: %w", err)
	}
	sum := sha256.Sum256(canonical.Bytes())
	if hex.EncodeToString(sum[:]) != wrapped.Digest {
		return fmt.Errorf("记录摘要校验失败")
	}
	if err := json.Unmarshal(wrapped.Data, target); err != nil {
		return err
	}
	return nil
}

func cloneCase(c *domain.ReleaseCase) (*domain.ReleaseCase, error) {
	if c == nil {
		return nil, nil
	}
	copy := *c
	copy.Participants = cloneParticipants(c.Participants)
	copy.Recordings = cloneRecordings(c.Recordings)
	copy.Consents = cloneConsents(c.Consents)
	copy.Findings = cloneFindings(c.Findings)
	copy.EvidencePackages = cloneEvidencePackages(c.EvidencePackages)
	if c.Decision != nil {
		decision := *c.Decision
		copy.Decision = &decision
	}
	if c.Manifest != nil {
		copy.Manifest = cloneManifest(c.Manifest)
	}
	if c.Credential != nil {
		credential := *c.Credential
		copy.Credential = &credential
	}
	return &copy, nil
}

func cloneParticipants(items []domain.Participant) []domain.Participant {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.Participant, len(items))
	copy(out, items)
	return out
}

func cloneRecordings(items []domain.RecordingItem) []domain.RecordingItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.RecordingItem, len(items))
	for i, r := range items {
		r.ParticipantIDs = cloneStrings(r.ParticipantIDs)
		r.LanguageTags = cloneStrings(r.LanguageTags)
		r.SensitiveTopics = cloneStrings(r.SensitiveTopics)
		r.Revisions = cloneRevisions(r.Revisions)
		out[i] = r
	}
	return out
}

func cloneRevisions(items []domain.RecordingRevision) []domain.RecordingRevision {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.RecordingRevision, len(items))
	copy(out, items)
	return out
}

func cloneConsents(items []domain.ConsentGrant) []domain.ConsentGrant {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.ConsentGrant, len(items))
	for i, g := range items {
		g.RecordingIDs = cloneStrings(g.RecordingIDs)
		g.AllowedPurposes = cloneStrings(g.AllowedPurposes)
		g.Audience = cloneStrings(g.Audience)
		g.SensitiveTopics = cloneStrings(g.SensitiveTopics)
		g.ExpiresAt = cloneTimePtr(g.ExpiresAt)
		g.WithdrawnAt = cloneTimePtr(g.WithdrawnAt)
		out[i] = g
	}
	return out
}

func cloneFindings(items []domain.ReviewFinding) []domain.ReviewFinding {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.ReviewFinding, len(items))
	for i, f := range items {
		f.ReviewedAt = cloneTimePtr(f.ReviewedAt)
		out[i] = f
	}
	return out
}

func cloneEvidencePackages(items []domain.EvidencePackage) []domain.EvidencePackage {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.EvidencePackage, len(items))
	for i, pkg := range items {
		pkg.FindingIDs = cloneStrings(pkg.FindingIDs)
		pkg.ConsentIDs = cloneStrings(pkg.ConsentIDs)
		pkg.RevisionIDs = cloneStrings(pkg.RevisionIDs)
		pkg.MaterialSummaries = append([]domain.EvidenceSummary(nil), pkg.MaterialSummaries...)
		pkg.RevisionSummaries = append([]domain.EvidenceSummary(nil), pkg.RevisionSummaries...)
		out[i] = pkg
	}
	return out
}

func cloneManifest(m *domain.ReleaseManifest) *domain.ReleaseManifest {
	if m == nil {
		return nil
	}
	out := *m
	out.Recordings = cloneManifestRecordings(m.Recordings)
	out.Consents = cloneManifestConsents(m.Consents)
	out.AccessScopes = cloneAccessScopes(m.AccessScopes)
	return &out
}

func cloneManifestRecordings(items []domain.ManifestRecording) []domain.ManifestRecording {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.ManifestRecording, len(items))
	for i, r := range items {
		r.ParticipantIDs = cloneStrings(r.ParticipantIDs)
		out[i] = r
	}
	return out
}

func cloneManifestConsents(items []domain.ManifestConsent) []domain.ManifestConsent {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.ManifestConsent, len(items))
	for i, c := range items {
		c.RecordingIDs = cloneStrings(c.RecordingIDs)
		out[i] = c
	}
	return out
}

func cloneAccessScopes(items []domain.AccessScope) []domain.AccessScope {
	if len(items) == 0 {
		return nil
	}
	out := make([]domain.AccessScope, len(items))
	for i, s := range items {
		s.AllowedPurposes = cloneStrings(s.AllowedPurposes)
		s.Audience = cloneStrings(s.Audience)
		s.SensitiveTopics = cloneStrings(s.SensitiveTopics)
		s.Reasons = cloneStrings(s.Reasons)
		s.ExpiresAt = cloneTimePtr(s.ExpiresAt)
		out[i] = s
	}
	return out
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	copy(out, items)
	return out
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	value := *t
	return &value
}
func safeName(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
}
