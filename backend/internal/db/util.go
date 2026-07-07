package db

import "github.com/google/uuid"

func uuidV4() string {
	return uuid.New().String()
}
