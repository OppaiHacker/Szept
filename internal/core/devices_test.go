package core

import (
	"bytes"
	"crypto/ed25519"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"szept/internal/connector"
	"szept/internal/event"
	"szept/internal/identity"
)

func TestVerifyLink(t *testing.T) {
	acct, dev, other := identity.Create(), identity.Create(), identity.Create()
	stmt := event.LinkStatement(acct.Pub, dev.Pub)
	proof := event.LinkProof(acct.Pub, ed25519.Sign(acct.Priv, stmt), ed25519.Sign(dev.Priv, stmt))
	if !bytes.Equal(event.VerifyLink(proof, dev.Pub), acct.Pub) {
		t.Fatal("valid proof rejected")
	}
	if event.VerifyLink(proof, other.Pub) != nil {
		t.Fatal("proof accepted for another device")
	}
	// an account alone cannot claim someone else's key
	half := event.LinkProof(acct.Pub, ed25519.Sign(acct.Priv, event.LinkStatement(acct.Pub, other.Pub)), ed25519.Sign(acct.Priv, event.LinkStatement(acct.Pub, other.Pub)))
	if event.VerifyLink(half, other.Pub) != nil {
		t.Fatal("proof without the device signature accepted")
	}
}

// A phone links to the PC's account: it lands in the PC's DM with a friend,
// reads and writes there, and the friend still sees one person. Unlinking
// removes the phone and keeps the PC in.
func TestLinkedDevice(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	pc := newCore(t, "ola", url)
	friend := newCore(t, "bartek", url)
	if _, err := pc.AddFriend(friend.FriendCode()); err != nil {
		t.Fatal(err)
	}
	if _, err := friend.AddFriend(pc.FriendCode()); err != nil {
		t.Fatal(err)
	}
	fs, _ := pc.St.Friends()
	go pc.ensurePeer(fs[0].Pub)
	waitFor(t, "pc and friend link up", 20*time.Second, func() bool { return pc.Online(friend.ID.Pub) })
	dm, err := pc.CreateDirect(fs[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.Send(dm, "z komputera"); err != nil {
		t.Fatal(err)
	}

	phone := newCore(t, "", url)
	code, err := pc.NewLinkCode()
	if err != nil {
		t.Fatal(err)
	}
	if err := phone.JoinAccount(code, "telefon"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "phone linked", 20*time.Second, func() bool { return phone.Linked() })
	if !bytes.Equal(phone.Account(), pc.ID.Pub) || phone.ID.Name != "ola" || !phone.St.IsFriend(friend.ID.Pub) {
		t.Fatalf("phone after link: account %x, name %q, friend %v", phone.Account()[:4], phone.ID.Name, phone.St.IsFriend(friend.ID.Pub))
	}
	if _, err := pc.NewLinkCode(); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.NewLinkCode(); err == nil {
		t.Fatal("a linked device issued a link code")
	}
	if phone.FriendCode() != pc.FriendCode() { // same connector, so the same code
		t.Fatal("a linked device hands out its own key, not the account")
	}

	waitFor(t, "phone gets the DM history", 30*time.Second, func() bool {
		for _, m := range phone.Messages(dm) {
			if m.Text == "z komputera" && bytes.Equal(m.Who, pc.ID.Pub) {
				return true
			}
		}
		return false
	})
	if err := phone.Send(dm, "z telefonu"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "friend gets the phone's message as from ola", 20*time.Second, func() bool {
		for _, m := range friend.Messages(dm) {
			if m.Text == "z telefonu" && bytes.Equal(m.Who, pc.ID.Pub) && m.Name == "ola" {
				return true
			}
		}
		return false
	})
	waitFor(t, "pc sees its phone's message as its own", 20*time.Second, func() bool {
		for _, m := range pc.Messages(dm) {
			if m.Text == "z telefonu" {
				return pc.Mine(m.Author)
			}
		}
		return false
	})
	for _, c := range []*Core{pc, friend, phone} {
		for _, ci := range c.Chats() {
			if !bytes.Equal(ci.ID, dm) {
				continue
			}
			if !ci.Direct || len(ci.Members) != 2 {
				t.Fatalf("%s sees the DM as direct=%v with %d members", c.ID.Name, ci.Direct, len(ci.Members))
			}
		}
	}

	// a group the friend owns, joined from the phone: the phone brings the pc in
	group, err := friend.CreateChat("ekipa", false)
	if err != nil {
		t.Fatal(err)
	}
	inv, pin, err := friend.NewInvite(group)
	if err != nil {
		t.Fatal(err)
	}
	if err := phone.Redeem(inv, pin); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "pc lands in the group the phone joined", 30*time.Second, func() bool {
		st, err := pc.ChatState(group)
		return err == nil && st.IsMember(pc.ID.Pub) && st.IsMember(phone.ID.Pub)
	})

	waitFor(t, "friend sees the invited phone as ola's device", 40*time.Second, func() bool {
		st, err := friend.ChatState(group)
		return err == nil && st.Members[string(phone.ID.Pub)] != nil && bytes.Equal(st.Members[string(phone.ID.Pub)].Of, pc.ID.Pub)
	})

	// unlink: the pc owns the DM and removes the phone itself; the group's
	// owner is asked to, and lets the pc back in after the kick cascades
	if err := pc.Unlink(phone.ID.Pub); err != nil {
		t.Fatal(err)
	}
	for _, chat := range [][]byte{dm, group} {
		waitFor(t, "friend sees the phone removed, pc still in", 40*time.Second, func() bool {
			st, err := friend.ChatState(chat)
			return err == nil && !st.IsMember(phone.ID.Pub) && st.IsMember(pc.ID.Pub)
		})
	}
	if pc.Mine(phone.ID.Pub) {
		t.Fatal("unlinked phone still counts as mine")
	}
}
