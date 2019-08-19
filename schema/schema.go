package schema

type User struct {
	Uid         string     `json:"uid,omitempty"`
	Username    string     `json:"username,omitempty"`
	Name        string     `json:"name,omitempty"`
	Urls        []string   `json:"urls,omitempty"`
	Likes       []Document `json:"likes,omitempty"`
	LikesRating int        `json:"~likes|rating,omitempty"`
}

type Document struct {
	Uid         string   `json:"uid,omitempty"`
	Url         string   `json:"url,omitempty"`
	Name        string   `json:"title,omitempty"`
	Author      string   `json:"author,omitempty"`
	Created     int      `json:"created,omitempty"`
	Updated     int      `json:"updated,omitempty"`
	Reviews     int      `json:"reviews,omitempty"`
	LikeCount   int      `json:"likecount,omitempty"`
	WordCount   int      `json:"wordcount,omitempty"`
	Chapters    int      `json:"chapters,omitempty"`
	Complete    bool     `json:"complete,omitempty"`
	Desc        string   `json:"desc,omitempty"`
	Image       string   `json:"image,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Likes       []User   `json:"~likes,omitempty"`
	LikesRating int      `json:"likes|rating,omitempty"`
	ISBN        int      `json:"isbn,omitempty"`
}
