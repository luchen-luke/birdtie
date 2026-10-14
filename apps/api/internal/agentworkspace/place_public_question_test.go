package agentworkspace

import "testing"

func TestPlacePublicQuestionCurrentSubjectAndTopicReplacement(t *testing.T) {
	current := placeFollowupTask()
	current.Filters[PlaceFollowupTopicFilter] = PlaceOpeningHours
	current.Filters["timePreference"] = "weekend"
	for _, sample := range []struct{ question, topic, suffix string }{
		{"它需要买票吗？", PlaceAdmission, " tickets admission booking"},
		{"收费吗？", PlaceAdmission, " tickets admission booking"},
		{"Does it need tickets?", PlaceAdmission, " tickets admission booking"},
		{"那里有轮椅入口吗？", PlaceAccessibility, " accessibility wheelchair access"},
		{"Is it wheelchair accessible?", PlaceAccessibility, " accessibility wheelchair access"},
		{"How do I get there?", PlaceTransport, " address directions public transport parking"},
		{"这个地方有停车场吗？", PlaceTransport, " address directions public transport parking"},
		{"它适合带孩子去吗？", PlacePublicInformation, ""},
		{"这个地方为什么值得参观？", PlacePublicInformation, ""},
		{"Is it suitable for children?", PlacePublicInformation, ""},
	} {
		t.Run(sample.question, func(t *testing.T) {
			followup, ok := ParsePlaceFollowup(sample.question, &current)
			intent := ParseMVPIntent(sample.question, &current)
			term, valid := PlaceQuestionSearchTerm("Aberdeen Art Gallery", followup.Topic)
			if !ok || !followup.Pronoun || followup.Topic != sample.topic || followup.TimePreference != "weekend" || !intent.Supported || intent.Operation != FindPlace || intent.SearchTerm != "Aberdeen Art Gallery" || !valid || term != "Aberdeen Art Gallery"+sample.suffix {
				t.Fatal("current public question lost canonical subject or inherited old hours topic", followup, intent, term)
			}
			if current.Filters[PlaceFollowupTopicFilter] != PlaceOpeningHours || current.Filters["searchTerm"] != "Aberdeen Art Gallery" {
				t.Fatal("parser mutated prior task state")
			}
		})
	}
}

func TestPlacePublicQuestionNamedFollowupKeepsOnlyValidatedTopic(t *testing.T) {
	for _, topic := range []string{PlaceAdmission, PlaceAccessibility, PlaceTransport, PlacePublicInformation} {
		current := placeFollowupTask()
		current.Filters[PlaceFollowupTopicFilter] = topic
		got, ok := ParsePlaceFollowup("那 Maritime Museum 呢？", &current)
		if !ok || got.Name != "Maritime Museum" || got.Pronoun || got.Topic != topic {
			t.Fatal("named replacement lost current validated public topic", got)
		}
	}
}

func TestPlacePublicQuestionDoesNotRoutePrivateOrOtherOperations(t *testing.T) {
	current := placeFollowupTask()
	for _, question := range []string{
		"它的私聊里说什么？", "它的私人记忆在哪里？", "它的用户住址在哪里？",
		"What does it know about my private profile?", "Can it send message for me?",
		"它帮我预约门票", "Book it for me?", "找活动 免费 羽毛球", "找公开成员 地址",
		"find activity tickets", "更近一点", "它是一个地方", "搜索此区域",
	} {
		if _, ok := ParsePlaceFollowup(question, &current); ok {
			t.Fatal("private data or original operation acquired place-question route", question)
		}
	}
	if _, ok := PlaceQuestionSearchTerm("Gallery", "PRIVATE_MEMORY"); ok {
		t.Fatal("arbitrary topic became search words")
	}
}
