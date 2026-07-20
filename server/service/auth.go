package service

import (
	"errors"
	"gobang/server/dao"
	"gobang/server/model"
	"strings"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	users *dao.UserDAO
}

func NewAuthService(users *dao.UserDAO) *AuthService {
	return &AuthService{users: users}
}

func (s *AuthService) Register(username, password string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	if _, err := s.users.FindByUsername(ctx, username); err == nil {
		return nil, errors.New("用户名已存在")
	} else if err != mongo.ErrNoDocuments {
		return nil, err
	}
	userID, err := s.users.NextUserID(ctx)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &model.User{
		UserID:       userID,
		Username:     username,
		PasswordHash: string(hash),
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AuthService) Login(username, password string) (*model.User, error) {
	ctx, cancel := dao.TimeoutContext()
	defer cancel()
	user, err := s.users.FindByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	return user, nil
}
