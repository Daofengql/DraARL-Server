package main

import (
	"errors"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/interconnect"
	"draarl/internal/protocol"
)

func TestGhostRecoveryTicketBindsSessionAndControlOwner(t *testing.T) {
	signer, err := newGhostRecoveryTicketSigner("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	session := ghostsession.Session{
		SessionID: "11111111-1111-4111-8111-111111111111", SessionTag: 0x10203040,
		ClientInstanceID: "22222222-2222-4222-8222-222222222222", OwnerID: 7,
		SSID: protocol.SSIDGhostAndroid, DevModel: protocol.DraARLDevModelAndroid, Transport: ghostsession.TransportEdge,
	}
	now := time.Now()
	ticket, err := signer.Sign(session, "edge-a", 99, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(ticket, now)
	if err != nil {
		t.Fatal(err)
	}
	item := interconnect.DeviceSessionConfirmItem{
		ControlSessionID: 99, OwnerID: 7, SSID: session.SSID, DevModel: session.DevModel,
		GhostSessionID: session.SessionID, ClientInstanceID: session.ClientInstanceID, RecoveryTicket: ticket,
	}
	if !claims.Matches("edge-a", item) {
		t.Fatal("valid ticket did not match its edge session proof")
	}
	if claims.Matches("edge-b", item) {
		t.Fatal("ticket crossed node ownership")
	}
	item.ControlSessionID++
	if claims.Matches("edge-a", item) {
		t.Fatal("ticket crossed control session ownership")
	}
}

func TestGhostRecoveryTicketRejectsTamperingAndExpiry(t *testing.T) {
	signer, _ := newGhostRecoveryTicketSigner("0123456789abcdef0123456789abcdef")
	session := ghostsession.Session{
		SessionID: "11111111-1111-4111-8111-111111111111", SessionTag: 1,
		ClientInstanceID: "22222222-2222-4222-8222-222222222222", OwnerID: 7,
		SSID: protocol.SSIDGhostAndroid, DevModel: protocol.DraARLDevModelAndroid, Transport: ghostsession.TransportEdge,
	}
	now := time.Now()
	ticket, err := signer.Sign(session, "edge-a", 99, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	replacement := byte('A')
	if ticket[0] == replacement {
		replacement = 'B'
	}
	tampered := string(replacement) + ticket[1:]
	if _, err := signer.Verify(tampered, now); !errors.Is(err, errInvalidGhostRecoveryTicket) {
		t.Fatalf("tampered ticket error=%v", err)
	}
	if _, err := signer.Verify(ticket, now.Add(2*time.Second)); !errors.Is(err, errInvalidGhostRecoveryTicket) {
		t.Fatalf("expired ticket error=%v", err)
	}
}
