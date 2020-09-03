package main

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"

	"github.com/cyverse-de/queries"
)

// BagsAPI provides an API for interacting with bags.
type BagsAPI struct {
	db *sql.DB
}

// BagRecord represents a bag as stored in the database.
type BagRecord struct {
	ID       string      `json:"id"`
	Contents BagContents `json:"contents"`
	UserID   string      `json:"user_id"`
}

// DefaultBag represents the default bag for a user.
type DefaultBag struct {
	UserID string `json:"user_id"`
	BagID  string `json:"bag_id"`
}

// BagContents represents a bag's contents stored in the database.
type BagContents map[string]interface{}

// Value ensures that the BagContents type implements the driver.Valuer interface.
func (b BagContents) Value() (driver.Value, error) {
	return json.Marshal(b)
}

// Scan implements the sql.Scanner interface for *BagContents
func (b *BagContents) Scan(value interface{}) error {
	valueBytes, ok := value.([]byte) //make sure that value can be type asserted to a []byte.
	if !ok {
		return errors.New("failed to cast value to []byte")
	}
	return json.Unmarshal(valueBytes, &b)
}

// GetUserID returns the user UUID for the provided username
func (b *BagsAPI) GetUserID(username string) (string, error) {
	var err error
	query := `SELECT users.id
				FROM users
			   WHERE users.username = $1`
	var userID string
	if err = b.db.QueryRow(query, username).Scan(&userID); err != nil {
		return "", err
	}
	return userID, err
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

// HasDefaultBag returns true if the user has a default bag.
func (b *BagsAPI) HasDefaultBag(username string) (bool, error) {
	query := `SELECT count(*)
				FROM default_bags d
					 users u
			   WHERE d.user_id = u.id
				 AND u.username = $1`
	var count int64
	if err := b.db.QueryRow(query, username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil

}

// HasBag returns true if the specified bag exists in the database.
func (b *BagsAPI) HasBag(username, bagID string) (bool, error) {
	query := `SELECT count(*)
				FROM bags b,
					 users u
			   WHERE b.user_id = u.id
				 AND u.username = $1
				 AND b.id = $2`
	var count int64
	if err := b.db.QueryRow(query, username, bagID).Scan(&count); err != nil {
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

	bagList := []BagRecord{}
	for rows.Next() {
		record := BagRecord{}
		err = rows.Scan(&record.ID, &record.Contents, &record.UserID)
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
				 AND u.username = $2
				 AND b.id = $1`
	var record BagRecord
	err := b.db.QueryRow(query, bagID, username).Scan(&record.ID, &record.Contents, &record.UserID)
	if err != nil {
		return record, err
	}
	return record, nil

}

func (b *BagsAPI) createDefaultBag(username string) (BagRecord, error) {
	var (
		err         error
		record      BagRecord
		newBagID    string
		newContents []byte
		userID      string
	)
	defaultContents := map[string]interface{}{}
	record.Contents = defaultContents

	if newContents, err = json.Marshal(defaultContents); err != nil {
		return record, err
	}

	if newBagID, err = b.AddBag(username, string(newContents)); err != nil {
		return record, err
	}

	record.ID = newBagID

	if err = b.SetDefaultBag(username, newBagID); err != nil {
		return record, err
	}

	if userID, err = b.GetUserID(username); err != nil {
		return record, err
	}

	record.UserID = userID

	return record, err
}

// GetDefaultBag returns the specified bag for the indicated user.
func (b *BagsAPI) GetDefaultBag(username string) (BagRecord, error) {
	var (
		err        error
		hasDefault bool
		record     BagRecord
	)

	// if the user doesn't have a default bag, add bag and set it as the default, then return it.
	if hasDefault, err = b.HasDefaultBag(username); err != nil {
		return record, err
	}

	if !hasDefault {
		return b.createDefaultBag(username)
	}

	query := `SELECT b.id,
					 b.contents,
					 b.user_id
				FROM bags b
				JOIN default_bags d ON (b.id = d.bag_id)
				JOIN users u ON (d.user_id = u.id)
			   WHERE u.username = $1`

	if err = b.db.QueryRow(query, username).Scan(&record.ID, &record.Contents, &record.UserID); err != nil {
		return record, err
	}

	return record, nil
}

// SetDefaultBag allows the user to update their default bag.
func (b *BagsAPI) SetDefaultBag(username, bagID string) error {
	var (
		err    error
		userID string
	)

	if userID, err = b.GetUserID(username); err != nil {
		return err
	}

	query := `INSERT INTO default_bags VALUES ( $1, $2 ) ON CONFLICT (user_id) DO UPDATE SET bag_id = $2`
	if _, err = b.db.Exec(query, userID, bagID); err != nil {
		return err
	}
	return nil

}

// AddBag adds (not updates) a new bag for the user. Returns the ID of the new bag record in the database.
func (b *BagsAPI) AddBag(username, contents string) (string, error) {
	query := `INSERT INTO bags (contents, user_id) VALUES ($1, $2) RETURNING id`

	userID, err := queries.UserID(b.db, username)
	if err != nil {
		return "", err
	}

	var bagID string
	if err = b.db.QueryRow(query, contents, userID).Scan(&bagID); err != nil {
		return "", err
	}

	return bagID, nil
}

// UpdateBag updates a specific bag with new contents.
func (b *BagsAPI) UpdateBag(username, bagID, contents string) error {
	query := `UPDATE ONLY bags SET contents = $1 WHERE id = $2 and user_id = $3`

	userID, err := queries.UserID(b.db, username)
	if err != nil {
		return err
	}

	if _, err = b.db.Exec(query, contents, bagID, userID); err != nil {
		return err
	}

	return nil
}

// UpdateDefaultBag updates the default bag with new content.
func (b *BagsAPI) UpdateDefaultBag(username, contents string) error {
	var (
		err        error
		defaultBag BagRecord
	)

	if defaultBag, err = b.GetDefaultBag(username); err != nil {
		return err
	}

	return b.UpdateBag(username, defaultBag.ID, contents)
}

// DeleteBag deletes the specified bag for the user.
func (b *BagsAPI) DeleteBag(username, bagID string) error {
	query := `DELETE FROM ONLY bags WHERE id = $1 and user_id = $2`

	userID, err := queries.UserID(b.db, username)
	if err != nil {
		return err
	}

	if _, err = b.db.Exec(query, bagID, userID); err != nil {
		return err
	}

	return nil
}

// DeleteDefaultBag deletes the default bag for the user. It will get
// recreated with nothing in it the next time it is retrieved through
// GetDefaultBag.
func (b *BagsAPI) DeleteDefaultBag(username string) error {
	var (
		err        error
		defaultBag BagRecord
	)

	if defaultBag, err = b.GetDefaultBag(username); err != nil {
		return err
	}

	return b.DeleteBag(username, defaultBag.ID)
}

// DeleteAllBags deletes all of the bags for the specified user.
func (b *BagsAPI) DeleteAllBags(username string) error {
	query := `DELETE FROM ONLY bags WHERE user_id = $1`

	userID, err := queries.UserID(b.db, username)
	if err != nil {
		return err
	}

	if _, err = b.db.Exec(query, userID); err != nil {
		return err
	}

	return nil
}
