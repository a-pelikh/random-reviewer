package core

import "errors"

var ErrNoUserMentioned = errors.New("no user mentioned")
var ErrUserAlreadyAdded = errors.New("user already added")
var ErrUserNotInReviewersList = errors.New("user not in reviewers list")
