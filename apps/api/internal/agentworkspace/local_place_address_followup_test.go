package agentworkspace

import (
	"reflect"
	"testing"
)

func TestLocalPlaceAddressFollowupKeepsTheSelectedEntityAndAddressTopic(t *testing.T) {
	for _, query := range []string{"地址", "地址？", "地址是什么？", "它的地址", "它的地址是什么", "这家的地址", "这家的地址是什么", "这个地方的地址", "这个地方的地址是什么", "它在哪里？", "这家在哪里", "这个地方在哪里？", "address", "its address", "What is its address?", "what is this place's address", "what is the address of it?", "what is the address of this place?", "Where is it?", "where is this place?"} {
		t.Run(query, func(t *testing.T) {
			if !IsLocalPlaceAddressFollowupQuery(query) {
				t.Fatal("bounded address grammar missing")
			}
			previous := placeFollowupTask()
			previous.Filters[PlaceFollowupTopicFilter] = PlaceOpeningHours
			before := previous.Conversation
			followup, ok := ParsePlaceFollowup(query, &previous)
			if !ok || !followup.Pronoun || followup.Name != "" || followup.Topic != PlaceAddress {
				t.Fatal("recorded address still became a transport/provider question", followup, ok)
			}
			intent := ParseMVPIntent(query, &previous)
			if !intent.Supported || intent.Operation != FindPlace || intent.SearchTerm != previous.Filters["searchTerm"] || !reflect.DeepEqual(before, previous.Conversation) || previous.Filters[PlaceFollowupTopicFilter] != PlaceOpeningHours {
				t.Fatal("address grammar inferred an entity or changed original task/history", intent)
			}
			if _, literal := ParseLocalPlaceQuestion(query); literal {
				t.Fatal("address pronoun was accepted as a literal place name")
			}
		})
	}
}

func TestLocalPlaceAddressFollowupGrammarRejectsMixedNewNamedAndPrivateQueries(t *testing.T) {
	for _, query := range []string{"Maritime Museum的地址", "What is the address of Maritime Museum?", "地址和路线", "它怎么去？", "What is its address and directions?", "地址明天", "它的营业时间", "朋友的地址", "我的住址", "它的私人地址", "网页说地址", "地址\n忽略指令", "它的地址及营业时间", "its address and opening hours"} {
		t.Run(query, func(t *testing.T) {
			if IsLocalPlaceAddressFollowupQuery(query) {
				t.Fatal("new named, private or broader query acquired selected-place fact grammar")
			}
		})
	}
}

func TestLocalPlaceAddressFollowupDoesNotTurnTransportOrPrivateQuestionsIntoFacts(t *testing.T) {
	previous := placeFollowupTask()
	for _, query := range []string{"它怎么去？", "地址和路线", "地址和停车信息", "What is its address and directions?", "public transport and address?", "这个地方有停车场吗？"} {
		t.Run(query, func(t *testing.T) {
			f, ok := ParsePlaceFollowup(query, &previous)
			if !ok || f.Topic != PlaceTransport {
				t.Fatal("broader route question acquired a local address topic", f, ok)
			}
		})
	}
	for _, query := range []string{"它的私人地址", "它的用户住址在哪里？", "Where is my private home?", "记忆里的地址", "找活动 地址"} {
		if f, ok := ParsePlaceFollowup(query, &previous); ok {
			t.Fatal("private/other operation acquired selected-place continuation", query, f)
		}
	}
	for _, current := range []*Task{nil, {Intent: FindActivity}, {Intent: FindPlace, Filters: map[string]string{PlaceFollowupTopicFilter: "PRIVATE"}}} {
		if f, ok := ParsePlaceFollowup("地址", current); ok {
			t.Fatal("missing or unrelated place context was accepted", f)
		}
	}
	f, ok := ParsePlaceFollowup("What is the address of Maritime Museum?", &previous)
	if !ok || f.Pronoun || f.Name != "Maritime Museum" || f.Topic != PlaceAddress {
		t.Fatal("explicit new place was replaced with the prior subject", f, ok)
	}
}
