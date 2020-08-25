package main

import (
	"database/sql"

	"github.com/cyverse-de/queries"
)

// BagsAPI provides an API for interacting with bags.
type BagsAPI struct {
	db *sql.DB
}

// BagRecord represents a bag as stored in the database.
type BagRecord struct {
	ID, Contents, UserID string
}

// HasBags returns true if the user has bags and false otherwise.
func (b *BagsAPI) HasBags(username string) (bool, error) {
	query := `SELECT count(*)
				FROM bags b,
					 users u
			   WHERE b.user_id = u.id
				 AND u.username = $1`
	var count int64
	if err := b.db.QueryRow(query, username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetBags returns all of the bags for the provided user.
func (b *BagsAPI) GetBags(username string) ([]BagRecord, error) {
	query := `SELECT b.id,
					 b.contents,
					 b.user_id
				FROM bags b,
					 users u
			   WHERE b.user_id = u.id
				 AND u.username = $1`

	rows, err := b.db.Query(query, username)
	if err != nil {
		return nil, err
	}

	var bagList []BagRecord
	for rows.Next() {
		record := BagRecord{}
		err = rows.Scan(&record.ID, record.Contents, record.UserID)
		if err != nil {
			return nil, err
		}

		bagList = append(bagList, record)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return bagList, nil
}

// GetBag returns the specified bag for the specified user according to the specified specifier for the
// bag record.
func (b *BagsAPI) GetBag(username, bagID string) (BagRecord, error) {
	query := `SELECT b.id,
					 b.contents,
					 b.user_id
				FROM bags b,
					 users u
			   WHERE b.user_id = u.id
				 AND b.username = $2
				 AND b.id = $1`
	var record BagRecord
	err := b.db.QueryRow(query, bagID, username).Scan(&record.ID, &record.Contents, &record.UserID)
	if err != nil {
		return record, err
	}
	return record, nil

}

// AddBag adds (not updates) a new bag for the user. Returns the ID of the new bag record in the database.
func (b *BagsAPI) AddBag(username, contents string) (string, error) {
	query := `INSERT INTO bags (contents, user_id) VALUES ($1, $2) RETURNING id`

	userID, err := queries.UserID(b.db, username)
	if err != nil {
		return "", err
	}

	var bagID string
	if err = b.db.QueryRow(query, userID).Scan(&bagID); err != nil {
		return "", err
	}

	return bagID, nil
}
