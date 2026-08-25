package auditlog

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
	"sync"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

type state struct {
	SchemaVersion  int                                 `json:"schemaVersion"`
	Events         []application.AuditEvent            `json:"events"`
	Credentials    map[string]domain.ReleaseCredential `json:"credentials"`
	ManifestIndex  map[string]string                   `json:"manifestIndex"`
	NextEvent      uint64                              `json:"nextEvent"`
	NextCredential uint64                              `json:"nextCredential"`
}

type diskState struct {
	Digest string          `json:"digest"`
	Data   json.RawMessage `json:"data"`
}

type Manager struct {
	path  string
	state state
	mu    sync.Mutex
}

func Open(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建审计目录: %w", err)
	}
	m := &Manager{path: filepath.Join(dir, "audit-chain.json"), state: state{SchemaVersion: 1, Credentials: map[string]domain.ReleaseCredential{}, ManifestIndex: map[string]string{}, NextEvent: 1, NextCredential: 1}}
	if err := m.load(); err != nil {
		return nil, err
	}
	valid, problems := verifyState(m.state)
	if !valid {
		return nil, fmt.Errorf("审计链损坏（原文件保留）: %v", problems)
	}
	return m, nil
}

func (m *Manager) Append(ctx context.Context, event application.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	event.Sequence = m.state.NextEvent
	if len(m.state.Events) > 0 {
		event.PreviousDigest = m.state.Events[len(m.state.Events)-1].Digest
	}
	event.Digest = auditDigest(event)
	m.state.Events = append(m.state.Events, event)
	m.state.NextEvent++
	if err := m.persist(m.state); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Timeline(ctx context.Context, caseID string) ([]application.AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []application.AuditEvent
	for _, event := range m.state.Events {
		if event.CaseID == caseID {
			result = append(result, event)
		}
	}
	return result, nil
}

func (m *Manager) IssueCredential(ctx context.Context, caseID, manifestDigest, issuedBy string, at time.Time) (domain.ReleaseCredential, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReleaseCredential{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if no, ok := m.state.ManifestIndex[manifestDigest]; ok {
		return m.state.Credentials[no], nil
	}
	next := cloneState(m.state)
	sequence := next.NextCredential
	no := fmt.Sprintf("OAR-%010d", sequence)
	previous := ""
	if sequence > 1 {
		previousNo := fmt.Sprintf("OAR-%010d", sequence-1)
		previous = next.Credentials[previousNo].CredentialDigest
	}
	credential := domain.ReleaseCredential{CredentialNo: no, CaseID: caseID, ManifestDigest: manifestDigest, PreviousDigest: previous, Sequence: sequence, IssuedBy: issuedBy, IssuedAt: at.UTC()}
	digest, err := domain.CredentialDigest(credential)
	if err != nil {
		return domain.ReleaseCredential{}, err
	}
	credential.CredentialDigest = digest
	next.Credentials[no] = credential
	next.ManifestIndex[manifestDigest] = no
	next.NextCredential++
	if err := m.persist(next); err != nil {
		return domain.ReleaseCredential{}, err
	}
	m.state = next
	return credential, nil
}

func (m *Manager) Credential(ctx context.Context, no string) (domain.ReleaseCredential, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReleaseCredential{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.state.Credentials[no]
	if !ok {
		return domain.ReleaseCredential{}, domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在")
	}
	return credential, nil
}

func (m *Manager) Verify(ctx context.Context) (bool, []string, error) {
	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	valid, problems := verifyState(m.state)
	return valid, problems, nil
}

func (m *Manager) CredentialSegment(ctx context.Context, target uint64, length int) ([]domain.ReleaseCredential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if length < 1 {
		return nil, domain.NewError("INVALID_SEGMENT_LENGTH", "区段长度必须为正整数")
	}
	if target < 1 || target >= m.state.NextCredential {
		return nil, domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在")
	}
	start := uint64(1)
	if uint64(length) <= target {
		start = target - uint64(length) + 1
	}
	result := make([]domain.ReleaseCredential, 0, target-start+1)
	for seq := start; seq <= target; seq++ {
		no := fmt.Sprintf("OAR-%010d", seq)
		credential, ok := m.state.Credentials[no]
		if !ok {
			return nil, domain.NewError("CREDENTIAL_CHAIN_GAP", "凭据链区段存在缺失节点")
		}
		result = append(result, credential)
	}
	return result, nil
}

func verifyState(s state) (bool, []string) {
	var problems []string
	previous := ""
	for i, event := range s.Events {
		if event.Sequence != uint64(i+1) {
			problems = append(problems, fmt.Sprintf("审计事件序号不连续：%d", event.Sequence))
		}
		if event.PreviousDigest != previous {
			problems = append(problems, fmt.Sprintf("审计事件 %d 前序摘要不匹配", event.Sequence))
		}
		if auditDigest(event) != event.Digest {
			problems = append(problems, fmt.Sprintf("审计事件 %d 摘要不匹配", event.Sequence))
		}
		previous = event.Digest
	}
	numbers := make([]string, 0, len(s.Credentials))
	for no := range s.Credentials {
		numbers = append(numbers, no)
	}
	sort.Strings(numbers)
	previous = ""
	for i, no := range numbers {
		credential := s.Credentials[no]
		if credential.Sequence != uint64(i+1) {
			problems = append(problems, "凭据序号不连续："+no)
		}
		if credential.PreviousDigest != previous {
			problems = append(problems, "凭据前序摘要不匹配："+no)
		}
		digest, err := domain.CredentialDigest(credential)
		if err != nil || digest != credential.CredentialDigest {
			problems = append(problems, "凭据摘要不匹配："+no)
		}
		if s.ManifestIndex[credential.ManifestDigest] != no {
			problems = append(problems, "凭据清单索引不匹配："+no)
		}
		previous = credential.CredentialDigest
	}
	return len(problems) == 0, problems
}

func auditDigest(event application.AuditEvent) string {
	event.Digest = ""
	b, _ := json.Marshal(event)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (m *Manager) load() error {
	b, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取审计链: %w", err)
	}
	var disk diskState
	if err := json.Unmarshal(b, &disk); err != nil {
		return fmt.Errorf("解析审计封套: %w", err)
	}
	var canonical bytes.Buffer
	if err := json.Compact(&canonical, disk.Data); err != nil {
		return fmt.Errorf("审计 data 不是有效 JSON: %w", err)
	}
	sum := sha256.Sum256(canonical.Bytes())
	if hex.EncodeToString(sum[:]) != disk.Digest {
		return fmt.Errorf("审计文件摘要校验失败")
	}
	if err := json.Unmarshal(disk.Data, &m.state); err != nil {
		return fmt.Errorf("解析审计状态: %w", err)
	}
	if m.state.SchemaVersion != 1 {
		return fmt.Errorf("不支持审计 schemaVersion %d", m.state.SchemaVersion)
	}
	if m.state.Credentials == nil {
		m.state.Credentials = map[string]domain.ReleaseCredential{}
	}
	if m.state.ManifestIndex == nil {
		m.state.ManifestIndex = map[string]string{}
	}
	return nil
}

func (m *Manager) persist(next state) error {
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	b, err := json.MarshalIndent(diskState{Digest: hex.EncodeToString(sum[:]), Data: data}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".audit-pending-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
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
	if err := os.Rename(name, m.path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(m.path))
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

func cloneState(source state) state {
	b, _ := json.Marshal(source)
	var target state
	_ = json.Unmarshal(b, &target)
	return target
}
