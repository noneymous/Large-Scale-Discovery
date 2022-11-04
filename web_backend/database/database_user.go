/*
* Large-Scale Discovery, a network scanning solution for information gathering in large IT/OT network environments.
*
* Copyright (c) Siemens AG, 2016-2021.
*
* This work is licensed under the terms of the MIT license. For a copy, see the LICENSE file in the top-level
* directory or visit <https://opensource.org/licenses/MIT>.
*
 */

package database

import (
	"database/sql"
	"errors"
	"github.com/microcosm-cc/bluemonday"
	"gorm.io/gorm"
	"strings"
	"time"
)

type T_user struct {
	// - Set the JSON ignore flag (json:"-") for sensitive columns that may NEVER be leaked by a JSON response.
	// - Make columns "not null" if possible. Otherwise, use null-types (e.g. sql.NullString).
	// - Avoid 'default' constraints or gorm will replace empty values (0, "", false) with set default values on CREATE!
	// - Define a lower-snake-case json name for every attribute.
	Id             uint64         `gorm:"column:id;primaryKey" json:"id"`
	Email          string         `gorm:"column:email;not null;unique" json:"email"`       // User ID. Notification e-mail == user ID, to make sure this is always in sync
	Password       sql.NullString `gorm:"column:password" json:"-"`                        // Password hash for users not using SAML/SSO
	SsoId          sql.NullString `gorm:"column:sso_id;unique" json:"-"`                   // Single Sign-On (SSO) ID, if not password login. Can also be used to match users to SSO requests
	Company        string         `gorm:"column:company;not null" json:"company"`          // Field to mark users of the same company, as those will be able to see each other
	Department     string         `gorm:"column:department;default:'';" json:"department"` // Field to support distinguishing users of a company from different departments
	Created        time.Time      `gorm:"column:created;not null" json:"created"`          //
	LastLogin      time.Time      `gorm:"column:last_login;not null" json:"last_login"`    // Last time an access token was requested
	LogoutCount    uint           `gorm:"column:logout_count;default:0" json:"-"`          // A counter incremented on each logout and incorporated into every JWT token to invalidate previously issued ones ahead of time.
	Active         bool           `gorm:"column:active;not null" json:"active"`            //
	Admin          bool           `gorm:"column:admin;not null" json:"admin"`              //
	Name           string         `gorm:"column:name;not null" json:"name"`                //
	Surname        string         `gorm:"column:surname;not null" json:"surname"`          //
	Gender         string         `gorm:"column:gender;default:''" json:"gender"`          // Gender could be either M/W/D, but can also be left empty
	Certificate    []byte         `gorm:"column:certificate;not null" json:"certificate"`  // User's public key to allow sending encrypted messages
	DbPasswordHash string         `gorm:"column:db_password;default:''" json:"-"`          // Hashed password generated by the system and used as the user's temporary password to access database views. This hash is injected into the database user object, to avoid clear-text password handling.

	Ownerships []T_ownership `gorm:"foreignKey:IdTUser" json:"ownerships"`
}

// NewUser constructs a User struct and pre-fills it with given or default data
func NewUser(email string, company string, department string, name string, surname string) *T_user {

	// Make sure users do not end up in one group un-intentionally. The authenticator must decide which users and
	// companies shall be grouped. Users within the same group may see e-mail addresses of other users in that group.
	if len(company) == 0 {
		company = email
	}

	// Create and return new user struct
	return &T_user{
		Email:       email,
		Company:     company,
		Department:  department,
		Created:     time.Now(),
		LogoutCount: 0,
		Active:      true,
		Admin:       false,
		Name:        name,
		Surname:     surname,
		Certificate: []byte{},
	}
}

// BeforeSave is a GORM hook that's executed every time the user object is written to the DB. This should be used to
// do some data sanitization, e.g. to strip illegal HTML tags in user attributes or to convert values to a certain
// format.
func (user *T_user) BeforeSave(tx *gorm.DB) error {

	// Initialize sanitizer
	b := bluemonday.StrictPolicy()

	// Sanitize value
	user.Email = b.Sanitize(user.Email)
	user.Email = strings.ToLower(user.Email) // Standard format of e-mail addresses shall be lower case.
	tx.Statement.SetColumn("email", user.Email)

	// Sanitize value
	if user.SsoId.Valid {
		ssoId := b.Sanitize(user.SsoId.String)
		ssoId = strings.ToUpper(ssoId) // Standard format of SAML ID shall be upper case.
		user.SsoId = sql.NullString{
			String: ssoId,
			Valid:  true,
		}
		tx.Statement.SetColumn("sso_id", user.SsoId)
	}

	// Sanitize value
	user.Name = b.Sanitize(user.Name)
	tx.Statement.SetColumn("name", user.Name)

	// Sanitize value
	user.Surname = b.Sanitize(user.Surname)
	tx.Statement.SetColumn("surname", user.Surname)

	// Sanitize value
	user.Gender = b.Sanitize(user.Gender)
	user.Gender = strings.ToUpper(user.Gender)
	if len(user.Gender) > 1 {
		user.Gender = user.Gender[:1]
	}
	tx.Statement.SetColumn("gender", user.Gender)

	// Return nil as everything went fine
	return nil
}

// Create crates a user in the database
func (user *T_user) Create() error {

	// Write user to database
	errDb := backendDb.Create(user).Error
	if errDb != nil {
		return errDb
	}

	// Return nil as everything went fine
	return nil
}

// Save updates defined columns of a user entry in the database. It updates defined columns, to the currently
// set values, even if the values are empty ones, such as 0, false or "".
// ATTENTION: Only update required columns to avoid overwriting changes of parallel processes (with data in memory)
func (user *T_user) Save(columns ...string) (int64, error) {

	// Verify that columns were supplied
	if len(columns) < 1 {
		return 0, nil
	}

	// Prevent the creation of new users
	if user.Id == 0 {
		return 0, errors.New("invalid entry ID")
	}

	// Prepare arguments to pass to GORM. Cannot pass string types, but interface types.
	// GORM requires some strange set of arguments
	var arg0 interface{} = columns[0]
	var args = make([]interface{}, 0, len(columns)-1)
	for _, column := range columns[1:] {
		args = append(args, column)
	}

	// Update user in database
	db := backendDb.
		Select(arg0, args...). // Select defines the columns to be updated
		Save(user)             // Save will also update empty values (false, 0, "")
	if db.Error != nil {
		return 0, db.Error
	}

	// Return nil as everything went fine
	return db.RowsAffected, nil
}

// Delete a user
func (user *T_user) Delete() error {

	// Delete user from database
	errDb := backendDb.Delete(user).Error
	if errDb != nil {
		return errDb
	}

	// Return nil as everything went fine
	return nil
}

// GetUsers gets all users from the db
func GetUsers() ([]T_user, error) {

	// Declare query results
	var entries = make([]T_user, 0, 3) // Initialize empty slice to avoid returning nil to frontend

	// Execute query
	errDb := backendDb.Find(&entries).Error
	if errDb != nil {
		return nil, errDb
	}

	// Return entries
	return entries, nil
}

// GetAdministrators gets all administrative users from the db
func GetAdministrators() ([]T_user, error) {

	// Declare query results
	var entries = make([]T_user, 0, 3) // Initialize empty slice to avoid returning nil to frontend

	// Execute query
	errDb := backendDb.
		Where("admin = true").
		Find(&entries).Error
	if errDb != nil {
		return nil, errDb
	}

	// Return entries
	return entries, nil
}

// GetUser searches a user by ID and returns a pointer to the found user. If no user is found, a nil pointer but no
// error will be returned.
func GetUser(id uint64) (*T_user, error) {

	// Declare query results
	var entries = make([]T_user, 0, 1)

	// Execute query
	errDb := backendDb.
		Preload("Ownerships").
		Preload("Ownerships.Group").
		Where("id = ?", id).
		Limit(1).
		Find(&entries).Error
	if errDb != nil {
		return nil, errDb
	}

	// Return nil if no entries were found
	if len(entries) < 1 {
		return nil, nil
	}

	// Return entries
	return &entries[0], nil
}

// GetUserByMail searches a user by e-mail address and returns a pointer to the found user. This function will only find
// zero or one user, because the e-mail address is a unique attribute. If no user is found, a nil pointer
// but no error will be returned.
func GetUserByMail(mail string) (*T_user, error) {

	// Declare query results
	var entries = make([]T_user, 0, 3) // Initialize empty slice to avoid returning nil to frontend

	// Convert to lower case
	mail = strings.ToLower(mail)

	// Execute query
	errDb := backendDb.Where("email = ?", mail).
		Limit(1).
		Find(&entries).Error
	if errDb != nil {
		return nil, errDb
	}

	// Return nil if no entries were found
	if len(entries) < 1 {
		return nil, nil
	}

	// Return entries
	return &entries[0], nil
}