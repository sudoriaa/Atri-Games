package data

import (
	"errors"
	"strings"
	"testing"
)

func socialFixture(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	store := newTestStore(t)
	alice, err := store.CreateUser("alice@example.test", "hash", "Alice")
	if err != nil {
		t.Fatalf("CreateUser alice: %v", err)
	}
	bob, err := store.CreateUser("bob@example.test", "hash", "Bob")
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	game, err := store.GameBySlug("neon-relay", "", true)
	if err != nil {
		t.Fatalf("GameBySlug: %v", err)
	}
	return store, game.ID, alice.ID, bob.ID
}

func TestGameLikesAreIdempotentAndPerUser(t *testing.T) {
	store, gameID, alice, bob := socialFixture(t)

	// A repeated like must not inflate the counter.
	for range 3 {
		if err := store.LikeGame(alice, gameID); err != nil {
			t.Fatalf("LikeGame: %v", err)
		}
	}
	count, liked, err := store.GameLikeState(alice, gameID)
	if err != nil {
		t.Fatalf("GameLikeState: %v", err)
	}
	if count != 1 || !liked {
		t.Fatalf("after repeated like: count=%d liked=%v, want 1/true", count, liked)
	}

	if err := store.LikeGame(bob, gameID); err != nil {
		t.Fatalf("LikeGame bob: %v", err)
	}
	count, _, err = store.GameLikeState("", gameID)
	if err != nil {
		t.Fatalf("GameLikeState anonymous: %v", err)
	}
	if count != 2 {
		t.Fatalf("two distinct likers = %d, want 2", count)
	}

	// An anonymous viewer never reports a personal like state.
	if _, anonLiked, _ := store.GameLikeState("", gameID); anonLiked {
		t.Fatal("anonymous viewer reported isLiked=true")
	}

	// Removing an absent like is not an error.
	if err := store.UnlikeGame(bob, gameID); err != nil {
		t.Fatalf("UnlikeGame: %v", err)
	}
	if err := store.UnlikeGame(bob, gameID); err != nil {
		t.Fatalf("repeat UnlikeGame: %v", err)
	}
	count, _, _ = store.GameLikeState(alice, gameID)
	if count != 1 {
		t.Fatalf("after unlike count = %d, want 1", count)
	}
}

func TestGameCountersSurfaceOnGameReads(t *testing.T) {
	store, gameID, alice, bob := socialFixture(t)

	if err := store.LikeGame(alice, gameID); err != nil {
		t.Fatalf("LikeGame: %v", err)
	}
	if _, err := store.RecordShare(gameID, alice, "card"); err != nil {
		t.Fatalf("RecordShare: %v", err)
	}
	if _, err := store.RecordShare(gameID, "", "link"); err != nil {
		t.Fatalf("anonymous RecordShare: %v", err)
	}
	root, err := store.CreateGameComment(alice, gameID, "", "第一条留言")
	if err != nil {
		t.Fatalf("CreateGameComment: %v", err)
	}
	if _, err := store.CreateGameComment(bob, gameID, root.ID, "回复"); err != nil {
		t.Fatalf("reply: %v", err)
	}

	game, err := store.GameBySlug("neon-relay", alice, true)
	if err != nil {
		t.Fatalf("GameBySlug: %v", err)
	}
	if game.LikeCount != 1 || !game.IsLiked {
		t.Fatalf("like fields = %d/%v, want 1/true", game.LikeCount, game.IsLiked)
	}
	if game.ShareCount != 2 {
		t.Fatalf("ShareCount = %d, want 2", game.ShareCount)
	}
	// CommentCount counts replies as well as root messages.
	if game.CommentCount != 2 {
		t.Fatalf("CommentCount = %d, want 2", game.CommentCount)
	}

	// The viewer-specific flag must not leak across users.
	asBob, err := store.GameBySlug("neon-relay", bob, true)
	if err != nil {
		t.Fatalf("GameBySlug bob: %v", err)
	}
	if asBob.IsLiked {
		t.Fatal("bob sees alice's like as his own")
	}

	// The same counters must appear through the list query, which binds the
	// viewer placeholders in a different order than the single-game read.
	list, err := store.Games(GameFilter{UserID: alice, Page: 1, PageSize: 24})
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	var found bool
	for _, item := range list.Items {
		if item.ID != gameID {
			continue
		}
		found = true
		if item.LikeCount != 1 || !item.IsLiked || item.ShareCount != 2 || item.CommentCount != 2 {
			t.Fatalf("list counters = %+v", item)
		}
	}
	if !found {
		t.Fatal("game missing from list results")
	}

	// Favourite listings bind a fifth viewer placeholder; guard that too.
	if err := store.AddFavorite(alice, gameID); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	favorites, err := store.FavoriteGames(alice)
	if err != nil {
		t.Fatalf("FavoriteGames: %v", err)
	}
	if len(favorites) != 1 || favorites[0].LikeCount != 1 || favorites[0].CommentCount != 2 {
		t.Fatalf("favorite counters = %+v", favorites)
	}
}

func TestGamesSortByLikes(t *testing.T) {
	store := newTestStore(t)
	users := make([]User, 0, 3)
	for _, email := range []string{"rank-a@example.test", "rank-b@example.test", "rank-c@example.test"} {
		user, err := store.CreateUser(email, "hash", "Ranked Player")
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		users = append(users, user)
	}
	for _, user := range users {
		if err := store.LikeGame(user.ID, "game_orbit"); err != nil {
			t.Fatalf("LikeGame(%s): %v", user.ID, err)
		}
	}
	if err := store.LikeGame(users[0].ID, "game_neon"); err != nil {
		t.Fatalf("LikeGame: %v", err)
	}

	list, err := store.Games(GameFilter{Sort: "likes", Page: 1, PageSize: 24})
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	if len(list.Items) < 2 || list.Items[0].ID != "game_orbit" || list.Items[1].ID != "game_neon" {
		t.Fatalf("unexpected like ordering: %+v", list.Items)
	}
}

func TestGamesSortByPlays(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.db.Exec(`UPDATE games SET play_count=10`); err != nil {
		t.Fatalf("normalize play counts: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE games SET play_count=99 WHERE id='game_orbit'`); err != nil {
		t.Fatalf("mark most-played game: %v", err)
	}

	list, err := store.Games(GameFilter{Sort: "plays", Page: 1, PageSize: 24})
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	if len(list.Items) == 0 || list.Items[0].ID != "game_orbit" {
		t.Fatalf("unexpected play ordering: %+v", list.Items)
	}
}

func TestGamesSortNewestByDefault(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.db.Exec(`UPDATE games SET published_at='2026-01-01T00:00:00Z',created_at='2026-01-01T00:00:00Z'`); err != nil {
		t.Fatalf("normalize game dates: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE games SET published_at='2026-02-01T00:00:00Z',created_at='2026-02-01T00:00:00Z' WHERE id='game_orbit'`); err != nil {
		t.Fatalf("mark newest game: %v", err)
	}

	for _, sort := range []string{"", "newest"} {
		list, err := store.Games(GameFilter{Sort: sort, Page: 1, PageSize: 24})
		if err != nil {
			t.Fatalf("Games(%q): %v", sort, err)
		}
		if len(list.Items) == 0 || list.Items[0].ID != "game_orbit" {
			t.Fatalf("newest ordering for %q = %+v", sort, list.Items)
		}
	}

	recommended, err := store.Games(GameFilter{Sort: "recommended", Page: 1, PageSize: 24})
	if err != nil {
		t.Fatalf("recommended Games: %v", err)
	}
	if len(recommended.Items) == 0 || recommended.Items[0].ID != "game_neon" {
		t.Fatalf("recommended ordering = %+v", recommended.Items)
	}

	if _, err := store.db.Exec(`UPDATE games SET published_at='2026-03-01T00:00:00Z',created_at='2026-03-01T00:00:00Z'`); err != nil {
		t.Fatalf("normalize tied game dates: %v", err)
	}
	var lastInsertedID string
	if err := store.db.QueryRow(`SELECT id FROM games ORDER BY rowid DESC LIMIT 1`).Scan(&lastInsertedID); err != nil {
		t.Fatalf("find last inserted game: %v", err)
	}
	tied, err := store.Games(GameFilter{Page: 1, PageSize: 24})
	if err != nil {
		t.Fatalf("tied Games: %v", err)
	}
	if len(tied.Items) == 0 || tied.Items[0].ID != lastInsertedID {
		t.Fatalf("same-second newest ordering = %+v, want %s first", tied.Items, lastInsertedID)
	}
}

func TestCommentThreadingRulesAndPermissions(t *testing.T) {
	store, gameID, alice, bob := socialFixture(t)

	root, err := store.CreateGameComment(alice, gameID, "", "  主留言  ")
	if err != nil {
		t.Fatalf("CreateGameComment: %v", err)
	}
	if root.Body != "主留言" {
		t.Fatalf("body not trimmed: %q", root.Body)
	}
	reply, err := store.CreateGameComment(bob, gameID, root.ID, "一层回复")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}

	// Nesting stops at one level.
	if _, err := store.CreateGameComment(alice, gameID, reply.ID, "二层回复"); !errors.Is(err, ErrCommentTooDeep) {
		t.Fatalf("nested reply error = %v, want ErrCommentTooDeep", err)
	}

	// Empty and oversized bodies are rejected.
	if _, err := store.CreateGameComment(alice, gameID, "", "   "); !errors.Is(err, ErrInvalidComment) {
		t.Fatalf("blank body error = %v, want ErrInvalidComment", err)
	}
	if _, err := store.CreateGameComment(alice, gameID, "", strings.Repeat("字", 1001)); !errors.Is(err, ErrInvalidComment) {
		t.Fatalf("oversized body error = %v, want ErrInvalidComment", err)
	}

	// A reply must belong to the same game as its parent.
	other, err := store.GameBySlug("echo-vault", "", true)
	if err != nil {
		t.Fatalf("GameBySlug echo-vault: %v", err)
	}
	if _, err := store.CreateGameComment(alice, other.ID, root.ID, "跨游戏回复"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-game reply error = %v, want ErrNotFound", err)
	}

	list, err := store.GameComments(gameID, bob, "user", 1, 20)
	if err != nil {
		t.Fatalf("GameComments: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("root paging = total %d items %d, want 1/1", list.Total, len(list.Items))
	}
	if len(list.Items[0].Replies) != 1 || list.Items[0].ReplyCount != 1 {
		t.Fatalf("replies = %+v", list.Items[0])
	}
	// Bob authored the reply but not the root comment.
	if list.Items[0].CanDelete {
		t.Fatal("bob may delete alice's root comment")
	}
	if !list.Items[0].Replies[0].CanDelete {
		t.Fatal("bob may not delete his own reply")
	}
	if list.Items[0].AuthorName != "Alice" {
		t.Fatalf("author name = %q", list.Items[0].AuthorName)
	}

	// A non-author non-admin cannot delete.
	if err := store.DeleteGameComment(bob, "user", gameID, root.ID); !errors.Is(err, ErrCommentForbidden) {
		t.Fatalf("delete by other user = %v, want ErrCommentForbidden", err)
	}
	// An administrator can.
	if err := store.DeleteGameComment(bob, "admin", gameID, root.ID); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	// Deleting a root cascades to its replies, so the thread empties out.
	after, err := store.GameComments(gameID, alice, "user", 1, 20)
	if err != nil {
		t.Fatalf("GameComments after delete: %v", err)
	}
	if after.Total != 0 || len(after.Items) != 0 {
		t.Fatalf("thread after cascade = %+v", after)
	}
	game, _ := store.GameBySlug("neon-relay", "", true)
	if game.CommentCount != 0 {
		t.Fatalf("CommentCount after cascade = %d, want 0", game.CommentCount)
	}
}

func TestCommentLikesScopedToGame(t *testing.T) {
	store, gameID, alice, bob := socialFixture(t)

	root, err := store.CreateGameComment(alice, gameID, "", "留言")
	if err != nil {
		t.Fatalf("CreateGameComment: %v", err)
	}

	count, err := store.LikeComment(bob, gameID, root.ID)
	if err != nil {
		t.Fatalf("LikeComment: %v", err)
	}
	if count != 1 {
		t.Fatalf("comment like count = %d, want 1", count)
	}
	// Repeating the like is idempotent.
	if count, err = store.LikeComment(bob, gameID, root.ID); err != nil || count != 1 {
		t.Fatalf("repeat LikeComment = %d, %v", count, err)
	}

	// A like routed through the wrong game must not apply.
	other, _ := store.GameBySlug("echo-vault", "", true)
	if _, err := store.LikeComment(bob, other.ID, root.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-game comment like = %v, want ErrNotFound", err)
	}

	list, err := store.GameComments(gameID, bob, "user", 1, 20)
	if err != nil {
		t.Fatalf("GameComments: %v", err)
	}
	if list.Items[0].LikeCount != 1 || !list.Items[0].IsLiked {
		t.Fatalf("viewer like state = %+v", list.Items[0])
	}
	asAlice, _ := store.GameComments(gameID, alice, "user", 1, 20)
	if asAlice.Items[0].IsLiked {
		t.Fatal("alice sees bob's comment like as her own")
	}

	if count, err = store.UnlikeComment(bob, gameID, root.ID); err != nil || count != 0 {
		t.Fatalf("UnlikeComment = %d, %v", count, err)
	}
}

func TestGameCommentsPaginationClampsPageSize(t *testing.T) {
	store, gameID, alice, _ := socialFixture(t)

	for i := range 3 {
		if _, err := store.CreateGameComment(alice, gameID, "", string(rune('a'+i))+" 留言"); err != nil {
			t.Fatalf("CreateGameComment %d: %v", i, err)
		}
	}

	// Out-of-range paging arguments fall back to safe defaults rather than
	// producing a negative OFFSET.
	list, err := store.GameComments(gameID, "", "", 0, 0)
	if err != nil {
		t.Fatalf("GameComments: %v", err)
	}
	if list.Page != 1 || list.PageSize != 20 || len(list.Items) != 3 {
		t.Fatalf("defaults = page %d size %d items %d", list.Page, list.PageSize, len(list.Items))
	}
	if list.Items[0].CanDelete {
		t.Fatal("anonymous viewer granted delete permission")
	}

	if list, err = store.GameComments(gameID, "", "", 1, 500); err != nil {
		t.Fatalf("GameComments oversized: %v", err)
	}
	if list.PageSize != 20 {
		t.Fatalf("oversized pageSize = %d, want clamp to 20", list.PageSize)
	}

	// Newest-first ordering across pages.
	page2, err := store.GameComments(gameID, "", "", 2, 2)
	if err != nil {
		t.Fatalf("GameComments page 2: %v", err)
	}
	if len(page2.Items) != 1 || page2.Total != 3 {
		t.Fatalf("page 2 = %d items, total %d", len(page2.Items), page2.Total)
	}
}
