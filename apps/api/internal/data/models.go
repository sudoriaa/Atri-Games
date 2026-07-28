package data

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	Status       string `json:"status"`
	CreatedAt    string `json:"createdAt"`
}

type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
	GameCount   int    `json:"gameCount,omitempty"`
}

type Game struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	Summary         string `json:"summary"`
	Description     string `json:"description"`
	AuthorName      string `json:"authorName"`
	CoverURL        string `json:"coverUrl"`
	LaunchURL       string `json:"launchUrl"`
	LaunchOpenIn    string `json:"launchOpenIn"`
	RepositoryURL   string `json:"repositoryUrl,omitempty"`
	Engine          string `json:"engine"`
	Version         string `json:"version"`
	Status          string `json:"status"`
	CategoryID      string `json:"categoryId"`
	CategoryName    string `json:"categoryName"`
	Featured        bool   `json:"featured"`
	NetworkRequired bool   `json:"networkRequired"`
	OwnBackend      bool   `json:"ownBackend"`
	RequiresLogin   bool   `json:"requiresLogin"`
	// UsesPlatformStorage and MatchmakingEnabled are capability hints for the
	// catalog UI. Detailed provider/scope/protocol settings remain in the
	// package manifest and are enforced by the platform endpoints.
	UsesPlatformStorage bool     `json:"usesPlatformStorage"`
	MatchmakingEnabled  bool     `json:"matchmakingEnabled"`
	Tags                []string `json:"tags"`
	PlayCount           int64    `json:"playCount"`
	FavoriteCount       int64    `json:"favoriteCount"`
	IsFavorite          bool     `json:"isFavorite"`
	LikeCount           int64    `json:"likeCount"`
	IsLiked             bool     `json:"isLiked"`
	CommentCount        int64    `json:"commentCount"`
	ShareCount          int64    `json:"shareCount"`
	CreatedAt           string   `json:"createdAt"`
	UpdatedAt           string   `json:"updatedAt"`
	PublishedAt         string   `json:"publishedAt,omitempty"`
}

// GameComment is one visible message in a game's discussion thread. Replies
// carry ParentID and are returned nested under their root comment.
type GameComment struct {
	ID          string        `json:"id"`
	GameID      string        `json:"gameId"`
	ParentID    string        `json:"parentId,omitempty"`
	AuthorID    string        `json:"authorId"`
	AuthorName  string        `json:"authorName"`
	AuthorRole  string        `json:"authorRole"`
	Body        string        `json:"body"`
	LikeCount   int64         `json:"likeCount"`
	IsLiked     bool          `json:"isLiked"`
	ReplyCount  int64         `json:"replyCount"`
	CanDelete   bool          `json:"canDelete"`
	CreatedAt   string        `json:"createdAt"`
	UpdatedAt   string        `json:"updatedAt"`
	Replies     []GameComment `json:"replies,omitempty"`
}

// GameCommentList is a page of root comments. Total counts root comments only,
// which is what the pager walks; CommentCount on Game counts replies too.
type GameCommentList struct {
	Items    []GameComment `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

type GameInput struct {
	Slug                string   `json:"slug"`
	Title               string   `json:"title"`
	Summary             string   `json:"summary"`
	Description         string   `json:"description"`
	AuthorName          string   `json:"authorName"`
	CoverURL            string   `json:"coverUrl"`
	LaunchURL           string   `json:"launchUrl"`
	LaunchOpenIn        string   `json:"launchOpenIn"`
	RepositoryURL       string   `json:"repositoryUrl"`
	Engine              string   `json:"engine"`
	Version             string   `json:"version"`
	Status              string   `json:"status"`
	CategoryID          string   `json:"categoryId"`
	Featured            bool     `json:"featured"`
	NetworkRequired     bool     `json:"networkRequired"`
	OwnBackend          bool     `json:"ownBackend"`
	RequiresLogin       bool     `json:"requiresLogin"`
	UsesPlatformStorage bool     `json:"usesPlatformStorage"`
	MatchmakingEnabled  bool     `json:"matchmakingEnabled"`
	Tags                []string `json:"tags"`
}

type LaunchResult struct {
	URL    string
	OpenIn string
}

type GameFilter struct {
	Query      string
	CategoryID string
	Status     string
	Featured   *bool
	Page       int
	PageSize   int
	Admin      bool
	UserID     string
}

type GameList struct {
	Items    []Game `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type DashboardMetrics struct {
	Users          int `json:"users"`
	PublishedGames int `json:"publishedGames"`
	ReviewGames    int `json:"reviewGames"`
	LaunchesToday  int `json:"launchesToday"`
	Favorites      int `json:"favorites"`
}

type Activity struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	ActorName  string `json:"actorName"`
	Detail     string `json:"detail"`
	CreatedAt  string `json:"createdAt"`
}
