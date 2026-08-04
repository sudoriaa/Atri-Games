package httpapi

import (
	"net/http"
	"testing"

	"github.com/sudoriaa/atri-games/apps/api/internal/data"
)

func TestCommunityP0Workflow(t *testing.T) {
	api := newTestAPI(t)
	creator := registerUser(t, api, "creator@example.test")
	follower := registerUser(t, api, "follower@example.test")
	admin := loginAdmin(t, api)

	profileUpdate := api.request(t, http.MethodPatch, "/api/v1/me", map[string]string{
		"displayName": "Creator One",
		"bio":         "使用 AI 和浏览器技术制作实验游戏。",
		"websiteUrl":  "https://creator.example.test",
	}, creator.Token)
	requireStatus(t, profileUpdate, http.StatusOK)
	updatedCreator := decodeResponse[data.User](t, profileUpdate)
	if updatedCreator.Bio == "" || updatedCreator.WebsiteURL == "" {
		t.Fatalf("profile update missing creator fields: %+v", updatedCreator)
	}

	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/creators/"+creator.User.ID+"/follow", nil, follower.Token), http.StatusOK)
	profileResponse := api.request(t, http.MethodGet, "/api/v1/creators/"+creator.User.ID, nil, follower.Token)
	requireStatus(t, profileResponse, http.StatusOK)
	profile := decodeResponse[data.CreatorProfile](t, profileResponse)
	if !profile.Following || profile.FollowerCount != 1 || profile.Bio == "" {
		t.Fatalf("unexpected creator profile: %+v", profile)
	}

	input := data.GameInput{
		Slug: "community-game", Title: "Community Game", Summary: "A community workflow test game.",
		Description: "A detailed community workflow test game description.", AuthorName: "Creator One",
		CoverURL: "https://assets.example.test/community.webp", LaunchURL: "https://play.example.test/community",
		LaunchOpenIn: "same-tab", Engine: "Canvas", Version: "1.0.0", Status: "review", CategoryID: "arcade",
		Tags: []string{"community"},
	}
	game, err := api.store.CreateGame(creator.User.ID, creator.User.ID, input)
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if err := api.store.RecordGameVersion(creator.User.ID, game, "首次社区版本"); err != nil {
		t.Fatalf("RecordGameVersion: %v", err)
	}
	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/admin/games/"+game.ID+"/approve", nil, admin.Token), http.StatusOK)

	feedResponse := api.request(t, http.MethodGet, "/api/v1/me/feed", nil, follower.Token)
	requireStatus(t, feedResponse, http.StatusOK)
	feed := decodeResponse[[]data.CommunityEvent](t, feedResponse)
	if len(feed) != 1 || feed[0].GameID != game.ID || feed[0].Kind != "game.published" {
		t.Fatalf("unexpected community feed: %+v", feed)
	}
	notificationResponse := api.request(t, http.MethodGet, "/api/v1/me/notifications", nil, follower.Token)
	requireStatus(t, notificationResponse, http.StatusOK)
	notifications := decodeResponse[data.NotificationList](t, notificationResponse)
	if notifications.UnreadCount == 0 || len(notifications.Items) == 0 {
		t.Fatalf("missing follower notification: %+v", notifications)
	}
	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/me/notifications/read", nil, follower.Token), http.StatusNoContent)

	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/games/community-game/follow", nil, follower.Token), http.StatusOK)
	gameFollow := decodeResponse[data.FollowState](t, api.request(t, http.MethodGet, "/api/v1/games/community-game/follow", nil, follower.Token))
	if !gameFollow.Following || gameFollow.FollowerCount != 1 {
		t.Fatalf("unexpected game follow state: %+v", gameFollow)
	}

	input.Version = "2.0.0"
	input.Title = "Community Game Updated"
	input.Status = "review"
	game, err = api.store.UpdateGame(creator.User.ID, game.ID, input)
	if err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}
	if err := api.store.RecordGameVersion(creator.User.ID, game, "第二版"); err != nil {
		t.Fatalf("RecordGameVersion v2: %v", err)
	}
	versionsResponse := api.request(t, http.MethodGet, "/api/v1/me/games/"+game.ID+"/versions", nil, creator.Token)
	requireStatus(t, versionsResponse, http.StatusOK)
	versions := decodeResponse[[]data.GameVersion](t, versionsResponse)
	if len(versions) != 2 || versions[0].Version != "2.0.0" || len(versions[0].Changes) == 0 {
		t.Fatalf("unexpected version history: %+v", versions)
	}
	rollbackResponse := api.request(t, http.MethodPost, "/api/v1/me/games/"+game.ID+"/versions/"+versions[1].ID+"/rollback", nil, creator.Token)
	requireStatus(t, rollbackResponse, http.StatusOK)
	rolledBack := decodeResponse[data.Game](t, rollbackResponse)
	if rolledBack.Version != "1.0.0" || rolledBack.Status != "review" {
		t.Fatalf("unexpected rollback result: %+v", rolledBack)
	}

	// Publish again so the game is a valid public report target.
	requireStatus(t, api.request(t, http.MethodPost, "/api/v1/admin/games/"+game.ID+"/approve", nil, admin.Token), http.StatusOK)
	reportResponse := api.request(t, http.MethodPost, "/api/v1/reports", map[string]string{
		"targetType": "game", "targetId": game.ID, "reason": "虚假或误导信息", "detail": "用于测试审核流程",
	}, follower.Token)
	requireStatus(t, reportResponse, http.StatusCreated)
	report := decodeResponse[data.ContentReport](t, reportResponse)
	reportsResponse := api.request(t, http.MethodGet, "/api/v1/admin/reports?status=pending", nil, admin.Token)
	requireStatus(t, reportsResponse, http.StatusOK)
	if reports := decodeResponse[[]data.ContentReport](t, reportsResponse); len(reports) != 1 || reports[0].ID != report.ID {
		t.Fatalf("unexpected moderation queue: %+v", reports)
	}
	resolvedResponse := api.request(t, http.MethodPatch, "/api/v1/admin/reports/"+report.ID, map[string]string{
		"status": "resolved", "resolution": "已核查并记录处理结果",
	}, admin.Token)
	requireStatus(t, resolvedResponse, http.StatusOK)
	if resolved := decodeResponse[data.ContentReport](t, resolvedResponse); resolved.Status != "resolved" || resolved.ResolvedByName == "" {
		t.Fatalf("unexpected resolved report: %+v", resolved)
	}

	myReportsResponse := api.request(t, http.MethodGet, "/api/v1/me/reports", nil, follower.Token)
	requireStatus(t, myReportsResponse, http.StatusOK)
	myReports := decodeResponse[[]data.ContentReport](t, myReportsResponse)
	if len(myReports) != 1 || myReports[0].ID != report.ID || myReports[0].Appeal != nil {
		t.Fatalf("unexpected personal moderation history: %+v", myReports)
	}
	appealResponse := api.request(t, http.MethodPost, "/api/v1/me/reports/"+report.ID+"/appeal", map[string]string{
		"reason": "请重新核对举报内容和处置依据",
	}, follower.Token)
	requireStatus(t, appealResponse, http.StatusCreated)
	appeal := decodeResponse[data.ModerationAppeal](t, appealResponse)
	if appeal.Status != "pending" || appeal.ReportID != report.ID {
		t.Fatalf("unexpected appeal: %+v", appeal)
	}
	appealsResponse := api.request(t, http.MethodGet, "/api/v1/admin/appeals?status=pending", nil, admin.Token)
	requireStatus(t, appealsResponse, http.StatusOK)
	if appeals := decodeResponse[[]data.ModerationAppeal](t, appealsResponse); len(appeals) != 1 || appeals[0].ID != appeal.ID {
		t.Fatalf("unexpected appeal queue: %+v", appeals)
	}
	acceptedResponse := api.request(t, http.MethodPatch, "/api/v1/admin/appeals/"+appeal.ID, map[string]string{
		"status": "accepted", "resolution": "同意重新复核，已将原举报退回待处理队列",
	}, admin.Token)
	requireStatus(t, acceptedResponse, http.StatusOK)
	if accepted := decodeResponse[data.ModerationAppeal](t, acceptedResponse); accepted.Status != "accepted" || accepted.ReportStatus != "resolved" || accepted.ResolvedByName == "" {
		t.Fatalf("unexpected accepted appeal: %+v", accepted)
	}
	reopenedResponse := api.request(t, http.MethodGet, "/api/v1/admin/reports?status=pending", nil, admin.Token)
	requireStatus(t, reopenedResponse, http.StatusOK)
	if reopened := decodeResponse[[]data.ContentReport](t, reopenedResponse); len(reopened) != 1 || reopened[0].ID != report.ID || reopened[0].Resolution == "" {
		t.Fatalf("accepted appeal did not reopen report: %+v", reopened)
	}

	blockResponse := api.request(t, http.MethodPost, "/api/v1/creators/"+creator.User.ID+"/block", nil, follower.Token)
	requireStatus(t, blockResponse, http.StatusOK)
	block := decodeResponse[data.BlockState](t, blockResponse)
	if !block.Blocked || block.Following || block.FollowerCount != 0 {
		t.Fatalf("unexpected block state: %+v", block)
	}
	blockedProfileResponse := api.request(t, http.MethodGet, "/api/v1/creators/"+creator.User.ID, nil, follower.Token)
	requireStatus(t, blockedProfileResponse, http.StatusOK)
	if blockedProfile := decodeResponse[data.CreatorProfile](t, blockedProfileResponse); !blockedProfile.Blocked || blockedProfile.Following {
		t.Fatalf("blocked creator relationship not reflected in profile: %+v", blockedProfile)
	}
	requireError(t, api.request(t, http.MethodPost, "/api/v1/creators/"+creator.User.ID+"/follow", nil, follower.Token), http.StatusUnprocessableEntity, "invalid_follow")
	blockedFeedResponse := api.request(t, http.MethodGet, "/api/v1/me/feed", nil, follower.Token)
	requireStatus(t, blockedFeedResponse, http.StatusOK)
	if blockedFeed := decodeResponse[[]data.CommunityEvent](t, blockedFeedResponse); len(blockedFeed) != 0 {
		t.Fatalf("blocked creator remained in feed: %+v", blockedFeed)
	}
	unblockResponse := api.request(t, http.MethodDelete, "/api/v1/creators/"+creator.User.ID+"/block", nil, follower.Token)
	requireStatus(t, unblockResponse, http.StatusOK)
	if unblock := decodeResponse[data.BlockState](t, unblockResponse); unblock.Blocked {
		t.Fatalf("unexpected unblock state: %+v", unblock)
	}
}
