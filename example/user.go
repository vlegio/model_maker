package user

//go:generate model_maker -file=./user.go -struct=User -table=user -sql=./user.sql

import (
	"time"
)

type User struct {
	ID          int64     `db:"id",gen:"bigint,autoincrement,notnull,primary"`
	Name        string    `db:"name",gen:"varchar(512),notnull"`
	CreatedAt   time.Time `db:"dt_created",gen:"datetime,notnull"`
	LastLoginAt time.Time `db:"dt_last_login",gen:"datetime,notnull"`
	Login       string    `db:"login",gen:"varchar(512),notnull,unique,index"`
	PwdHash     string    `db:"pwd_hash",gen:"varchar(512),notnull"`
}
