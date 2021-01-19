package db

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"time"
)

type Config struct {
	Username string
	Password string
	ServerAddress string
	ServerPort string
}

type Mongo interface {
	Close() error
	UpdateStateTempClipById(id string, fromState State, toState State) error
	PushClipsInVideoById(id string, clips []Clip, duration int64) error
	MarkVideoAvailable(id string) error
}

type mgo struct {
	client *mongo.Client
	database *mongo.Database
	videosCollection *mongo.Collection
	tempClipsCollection *mongo.Collection
}

func (m mgo) MarkVideoAvailable(id string) error {
	ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
	_id, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		panic(err)
	}
	filter := bson.D{{"_id", _id}}
	update := bson.D{
		{
			"$set", bson.D{{
			"available", true,
		}}},
	}

	result, err := m.videosCollection.UpdateOne(
		ctx,
		filter,
		update,
	)
	if err != nil {
		panic(err)
	}

	if result.ModifiedCount < 1 {
		return NothingModifiedError
	}

	return nil
}

func (m mgo) PushClipsInVideoById(id string, clips []Clip, duration int64) error {
	ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
	_id, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		panic(err)
	}
	filter := bson.D{{"_id", _id}}
	update := bson.D{{
		"$push", bson.D{{
			"clips", bson.D{{
				"$each", clips,
			}},
		}}},
		{
			"$set", bson.D{{
			"updated_timestamp", time.Now()},
			{
				"duration", duration,
			},
		}},
	}

	result, err := m.videosCollection.UpdateOne(
		ctx,
		filter,
		update,
	)
	if err != nil {
		panic(err)
	}

	if result.ModifiedCount < 1 {
		return NothingModifiedError
	}

	return nil
}

func (m* mgo) UpdateStateTempClipById(id string, fromState State, toState State) error {
	ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
	_id, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		panic(err)
	}
	filter := bson.D{{"_id", _id}, {"state", fromState}}
	update := bson.D{{
		"$set", bson.D{{
			"state", toState,
		}},
	}}
	result, err := m.tempClipsCollection.UpdateOne(
		ctx,
		filter,
		update,
	)
	if err != nil {
		panic(err)
	}

	if result.ModifiedCount < 1 {
		return NothingModifiedError
	}

	return nil
}

func (m* mgo) Close() error  {
	ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
	return m.client.Disconnect(ctx)
}

func NewMongo(c Config) Mongo {
	var uri string
	if c.Username == "" || c.Password == "" {
		uri = fmt.Sprintf("mongodb://%v:%v", c.ServerAddress, c.ServerPort)
	} else {
		uri = fmt.Sprintf("mongodb://%v:%v@%v:%v", c.Username, c.Password, c.ServerAddress, c.ServerPort)
	}

	client, err := mongo.NewClient(options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}

	ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
	err = client.Connect(ctx)
	if err != nil {
		panic(err)
	}

	database := client.Database("videoPlatform")
	videosCollection := database.Collection("videos")
	tempClipsCollection := database.Collection("tempclips")

	return &mgo{
		client: client,
		database: database,
		videosCollection: videosCollection,
		tempClipsCollection: tempClipsCollection,
	}
}