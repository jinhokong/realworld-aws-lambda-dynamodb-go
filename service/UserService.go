package service

import (
	"bytes"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/aws/aws-sdk-go/service/dynamodb/expression"
	"github.com/chrisxue815/realworld-aws-lambda-dynamodb-go/model"
	"github.com/chrisxue815/realworld-aws-lambda-dynamodb-go/util"
)

func PutUser(user model.User) error {
	err := user.Validate()
	if err != nil {
		return err
	}

	userItem, err := dynamodbattribute.MarshalMap(user)
	if err != nil {
		return err
	}

	emailUser := model.EmailUser{
		Email:    user.Email,
		Username: user.Username,
	}

	emailUserItem, err := dynamodbattribute.MarshalMap(emailUser)
	if err != nil {
		return err
	}

	// Put a new user, make sure username and email are unique
	transaction := dynamodb.TransactWriteItemsInput{
		TransactItems: []*dynamodb.TransactWriteItem{
			{
				Put: &dynamodb.Put{
					TableName:           aws.String(UserTableName.Get()),
					Item:                userItem,
					ConditionExpression: aws.String("attribute_not_exists(Username)"),
				},
			},
			{
				Put: &dynamodb.Put{
					TableName:           aws.String(EmailUserTableName.Get()),
					Item:                emailUserItem,
					ConditionExpression: aws.String("attribute_not_exists(Email)"),
				},
			},
		},
	}

	_, err = DynamoDB().TransactWriteItems(&transaction)
	if err != nil {
		// TODO: distinguish:
		// NewInputError("username", "has already been taken")
		// NewInputError("email", "has already been taken")
		return err
	}

	return nil
}

func UpdateUser(oldUser model.User, newUser model.User) error {
	err := newUser.Validate()
	if err != nil {
		return err
	}

	emailUser := model.EmailUser{
		Email:    newUser.Email,
		Username: newUser.Username,
	}

	emailUserItem, err := dynamodbattribute.MarshalMap(emailUser)
	if err != nil {
		return err
	}

	transactItems := make([]*dynamodb.TransactWriteItem, 0, 3)

	if oldUser.Email != newUser.Email {
		// Link user with the new email
		transactItems = append(transactItems, &dynamodb.TransactWriteItem{
			Put: &dynamodb.Put{
				TableName:           aws.String(EmailUserTableName.Get()),
				Item:                emailUserItem,
				ConditionExpression: aws.String("attribute_not_exists(Email)"),
			},
		})

		// Unlink user from the old email
		transactItems = append(transactItems, &dynamodb.TransactWriteItem{
			Delete: &dynamodb.Delete{
				TableName:           aws.String(EmailUserTableName.Get()),
				Key:                 StringKey("Email", oldUser.Email),
				ConditionExpression: aws.String("attribute_exists(Email)"),
			},
		})
	}

	expr, err := buildUserUpdateExpression(oldUser, newUser)
	if err != nil {
		return err
	}

	// No field changed
	if expr.Update() == nil {
		return nil
	}

	// Update user info
	transactItems = append(transactItems, &dynamodb.TransactWriteItem{
		Update: &dynamodb.Update{
			TableName:                 aws.String(UserTableName.Get()),
			Key:                       StringKey("Username", oldUser.Username),
			ConditionExpression:       aws.String("attribute_exists(Username)"),
			UpdateExpression:          expr.Update(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
		},
	})

	_, err = DynamoDB().TransactWriteItems(&dynamodb.TransactWriteItemsInput{
		TransactItems: transactItems,
	})
	if err != nil {
		return err
	}

	return nil
}

func buildUserUpdateExpression(oldUser model.User, newUser model.User) (expression.Expression, error) {
	update := expression.UpdateBuilder{}

	if oldUser.Email != newUser.Email {
		update = update.Set(expression.Name("Email"), expression.Value(newUser.Email))
	}

	if newUser.PasswordHash != nil && !bytes.Equal(oldUser.PasswordHash, newUser.PasswordHash) {
		update = update.Set(expression.Name("PasswordHash"), expression.Value(newUser.PasswordHash))
	}

	if oldUser.Image != newUser.Image {
		if newUser.Image != "" {
			update = update.Set(expression.Name("Image"), expression.Value(newUser.Image))
		} else {
			update = update.Remove(expression.Name("Image"))
		}
	}

	if oldUser.Bio != newUser.Bio {
		if newUser.Bio != "" {
			update = update.Set(expression.Name("Bio"), expression.Value(newUser.Bio))
		} else {
			update = update.Remove(expression.Name("Bio"))
		}
	}

	if IsUpdateBuilderEmpty(update) {
		return expression.Expression{}, nil
	}

	builder := expression.NewBuilder().WithUpdate(update)
	return builder.Build()
}

func GetUserByEmail(email string) (model.User, error) {
	if email == "" {
		return model.User{}, util.NewInputError("email", "can't be blank")
	}

	username, err := GetUsernameByEmail(email)
	if err != nil {
		return model.User{}, err
	}

	if username == "" {
		return model.User{}, util.NewInputError("email", "not found")
	}

	return GetUserByUsername(username)
}

func GetUsernameByEmail(email string) (string, error) {
	emailUser := model.EmailUser{}
	found, err := GetItemByKey(EmailUserTableName.Get(), StringKey("Email", email), &emailUser)

	if err != nil {
		return "", err
	}

	if !found {
		return "", util.NewInputError("email", "not found")
	}

	return emailUser.Username, nil
}

func GetUserByUsername(username string) (model.User, error) {
	user := model.User{}
	found, err := GetItemByKey(UserTableName.Get(), StringKey("Username", username), &user)

	if err != nil {
		return model.User{}, err
	}

	if !found {
		return model.User{}, util.NewInputError("username", "not found")
	}

	return user, err
}

func GetCurrentUser(auth string) (*model.User, string, error) {
	username, token, err := VerifyAuthorization(auth)
	if err != nil {
		return nil, "", err
	}

	user, err := GetUserByUsername(username)
	if err != nil {
		return nil, "", err
	}

	return &user, token, nil
}

func GetUserListByUsername(usernames []string) ([]model.User, error) {
	if len(usernames) == 0 {
		return make([]model.User, 0), nil
	}

	usernameSet := make(map[string]bool)
	for _, username := range usernames {
		usernameSet[username] = true
	}

	keys := make([]AWSObject, 0, len(usernameSet))
	for username := range usernameSet {
		keys = append(keys, StringKey("Username", username))
	}

	batchGetUsers := dynamodb.BatchGetItemInput{
		RequestItems: map[string]*dynamodb.KeysAndAttributes{
			UserTableName.Get(): {
				Keys: keys,
			},
		},
	}

	responses, err := BatchGetItems(&batchGetUsers, len(usernames))
	if err != nil {
		return nil, err
	}

	usersByUsername := make(map[string]model.User)

	for _, response := range responses {
		for _, items := range response {
			for _, item := range items {
				user := model.User{}
				err = dynamodbattribute.UnmarshalMap(item, &user)
				if err != nil {
					return nil, err
				}

				usersByUsername[user.Username] = user
			}
		}
	}

	users := make([]model.User, 0, len(usernames))
	for _, username := range usernames {
		users = append(users, usersByUsername[username])
	}

	return users, nil
}
