package domain

type CredentialChainNode struct {
	Sequence         uint64 `json:"sequence"`
	CredentialNo     string `json:"credentialNo"`
	PreviousDigest   string `json:"previousDigest"`
	CredentialDigest string `json:"credentialDigest"`
	CaseID           string `json:"caseId"`
	ManifestDigest   string `json:"manifestDigest"`
	Valid            bool   `json:"valid"`
	ProblemCode      string `json:"problemCode,omitempty"`
}

type CredentialChainResult struct {
	TargetCredentialNo   string                `json:"targetCredentialNo"`
	Nodes                []CredentialChainNode `json:"nodes"`
	Valid                bool                  `json:"valid"`
	FirstFailureSequence uint64                `json:"firstFailureSequence,omitempty"`
	ProblemCode          string                `json:"problemCode,omitempty"`
	TargetManifestValid  bool                  `json:"targetManifestValid"`
	TargetIndexValid     bool                  `json:"targetIndexValid"`
}

func VerifyCredentialSegment(credentials []ReleaseCredential) CredentialChainResult {
	return VerifyCredentialSegmentFrom(credentials, "", len(credentials) > 0 && credentials[0].Sequence == 1)
}

func VerifyCredentialSegmentFrom(credentials []ReleaseCredential, expectedPrevious string, checkFirst bool) CredentialChainResult {
	result := CredentialChainResult{Nodes: []CredentialChainNode{}, Valid: true, TargetManifestValid: true, TargetIndexValid: true}
	if len(credentials) == 0 {
		result.Valid = false
		result.ProblemCode = "CREDENTIAL_NOT_FOUND"
		return result
	}
	result.TargetCredentialNo = credentials[len(credentials)-1].CredentialNo
	for i, credential := range credentials {
		node := CredentialChainNode{Sequence: credential.Sequence, CredentialNo: credential.CredentialNo, PreviousDigest: credential.PreviousDigest, CredentialDigest: credential.CredentialDigest, CaseID: credential.CaseID, ManifestDigest: credential.ManifestDigest, Valid: true}
		digest, err := CredentialDigest(credential)
		if err != nil || digest != credential.CredentialDigest {
			node.Valid = false
			node.ProblemCode = "CREDENTIAL_DIGEST_MISMATCH"
		}
		if i == 0 && checkFirst && credential.PreviousDigest != expectedPrevious {
			node.Valid = false
			node.ProblemCode = "CREDENTIAL_PREVIOUS_DIGEST_MISMATCH"
		}
		if i > 0 {
			if credential.Sequence != credentials[i-1].Sequence+1 {
				node.Valid = false
				node.ProblemCode = "CREDENTIAL_SEQUENCE_GAP"
			}
			if credential.PreviousDigest != credentials[i-1].CredentialDigest {
				node.Valid = false
				node.ProblemCode = "CREDENTIAL_PREVIOUS_DIGEST_MISMATCH"
			}
		}
		if !node.Valid && result.Valid {
			result.Valid = false
			result.FirstFailureSequence = node.Sequence
			result.ProblemCode = node.ProblemCode
		}
		result.Nodes = append(result.Nodes, node)
	}
	return result
}
