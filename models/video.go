package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Video struct {
	ID                    primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	Title                 string              `bson:"title" json:"title"`
	FileID                string              `bson:"fileId" json:"fileId"`
	ThumbnailURL          string              `bson:"thumbnailUrl,omitempty" json:"thumbnailUrl,omitempty"`
	Category              string              `bson:"category,omitempty" json:"category,omitempty"`
	Visibility            string              `bson:"visibility,omitempty" json:"visibility,omitempty"`
	Uploader              primitive.ObjectID  `bson:"uploader,omitempty" json:"uploader,omitempty"`
	IsShort               bool                `bson:"isShort,omitempty" json:"isShort,omitempty"`
	Views                 int64               `bson:"views,omitempty" json:"views,omitempty"`
	LastRefreshedAt       *time.Time          `bson:"lastRefreshedAt,omitempty" json:"lastRefreshedAt,omitempty"`
	PendingRemoteUploadID *string             `bson:"pendingRemoteUploadId,omitempty" json:"pendingRemoteUploadId,omitempty"`
	StreamtapeStatus      string              `bson:"streamtapeStatus,omitempty" json:"streamtapeStatus,omitempty"`
	StreamtapeURL         string              `bson:"streamtapeUrl,omitempty" json:"streamtapeUrl,omitempty"`
	CreatedAt             time.Time           `bson:"createdAt,omitempty" json:"createdAt,omitempty"`
	UpdatedAt             time.Time           `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}
