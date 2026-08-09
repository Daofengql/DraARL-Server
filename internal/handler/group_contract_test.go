package handler

import "testing"

func TestGroupTypeContract(t *testing.T) {
	if !isSupportedGroupType(groupTypePublic) || !isSupportedGroupType(groupTypePrivate) {
		t.Fatal("public and private group types must remain supported")
	}
	if isSupportedGroupType(0) || isSupportedGroupType(99) {
		t.Fatal("unsupported group type was accepted")
	}
}

func TestGroupUpdateDTOKeepsOmittedFieldSemantics(t *testing.T) {
	req := UpdateGroupRequest{}
	if req.Name != "" || req.Type != 0 {
		t.Fatalf("empty update DTO changed defaults: %#v", req)
	}
}
