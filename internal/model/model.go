package model

type Text struct {
	ID     int64
	Label  string   // eg: "151" or "58–59"
	Body   string   // verse text
	Images []string // 0..n filesystem paths (we'll cap to 4 on post)
}

// --- v2 create tweet ---

type TweetReq struct {
	Text  string      `json:"text"`
	Media *TweetMedia `json:"media,omitempty"`
}
type TweetMedia struct {
	MediaIDs []string `json:"media_ids"`
}
type TweetResp struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

// --- v1.1 media/upload (simple upload) ---

type MediaUploadResp struct {
	MediaID       int64  `json:"media_id"`
	MediaIDString string `json:"media_id_string"`
}
