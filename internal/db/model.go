package db

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
)


type Object string
/** "_id": ObjectId("5fbf51422793fb67f8ced74b"),
    "ownerId": ObjectId("5fbf51422793fb67f8ced74a"),
    "object": "upload/videos/8c764550efb06ac761b61290d8cb19ad",
    "uploaded_timestamp": ISODate("2020-11-26T06:54:58Z"),
    "state": "Initial",
    "__v": NumberInt("0"),
    "videoId": ObjectId("5fbf51432793fb67f8ced74c")
*/
type State string
const (
	StateInitial State = "Initial"
	StateQueued State = "Queued"
	StateProcessing State = "Processing"
	StateFinished State = "Finished"
	StateFailed State = "Failed"
)

type TempClip struct {
	Id primitive.ObjectID `bson:"_id,omitempty"`
	OwnerId primitive.ObjectID `bson:"ownerId,omitempty"`
	Object Object `bson:"object,omitempty"`
	UploadedTimestamp primitive.DateTime `bson:"uploaded_timestamp,omitempty"`
	State State `bson:"state,omitempty"`
	VideoId primitive.ObjectID `bson:"videoId,omitempty"`
}

/** "_id": ObjectId("5fbf4dc6f5e9955443760392"),
    "tags": [
        "EDUCATION"
    ],
    "available": false,
    "owner": ObjectId("5fbf4dc6f5e9955443760390"),
    "title": "Test video",
    "created_timestamp": ISODate("2020-11-26T06:40:06Z"),
    "updated_timestamp": ISODate("2020-11-26T06:40:06Z"),
    "clips": [ ],
    "__v": NumberInt("0") */
type Video struct {
	Id primitive.ObjectID `bson:"_id,omitempty"`
	Available bool `bson:"available,omitempty"`
	Owner primitive.ObjectID `bson:"owner,omitempty"`
	Title string `bson:"title,omitempty"`
	CreatedTimestamp primitive.DateTime `bson:"created_timestamp,omitempty"`
	UpdatedTimestamp primitive.DateTime `bson:"updated_timestamp,omitempty"`
}

/** object: string;
  definition: typeof DEFINITIONS[number];
  thumbnails: string[];
*/
type Definition string
const (
	DefinitionFullHD Definition = "FullHD"
	DefinitionHD Definition = "HD"
	DefinitionSD Definition = "SD"
)

type Clip struct {
	Id primitive.ObjectID `bson:"_id,omitempty"`
	Object Object `bson:"object,omitempty"`
	Definition Definition `bson:"definition,omitempty"`
	Thumbnails []Object `bson:"thumbnails,omitempty"`
}