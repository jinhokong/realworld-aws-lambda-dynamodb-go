package main

import (
	"encoding/json"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/chrisxue815/realworld-aws-lambda-dynamodb-go/model"
	"github.com/chrisxue815/realworld-aws-lambda-dynamodb-go/service"
	"github.com/chrisxue815/realworld-aws-lambda-dynamodb-go/util"
)

type RequestBody struct {
	User UserRequest `json:"user"`
}

type UserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Image    string `json:"image"`
	Bio      string `json:"bio"`
}

type ResponseBody struct {
	User UserResponse `json:"user"`
}

type UserResponse struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Image    string `json:"image"`
	Bio      string `json:"bio"`
	Token    string `json:"token"`
}

func Handle(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	oldUser, token, err := service.GetCurrentUser(request.Headers["Authorization"])
	if err != nil {
		return util.NewUnauthorizedResponse()
	}

	requestBody := RequestBody{}
	err = json.Unmarshal([]byte(request.Body), &requestBody)
	if err != nil {
		return util.NewErrorResponse(err)
	}

	err = model.ValidatePassword(requestBody.User.Password)
	if err != nil {
		return util.NewErrorResponse(err)
	}

	passwordHash, err := service.Scrypt(requestBody.User.Password)
	if err != nil {
		return util.NewErrorResponse(err)
	}

	newUser := model.User{
		Username:     oldUser.Username,
		Email:        requestBody.User.Email,
		PasswordHash: passwordHash,
		Image:        requestBody.User.Image,
		Bio:          requestBody.User.Bio,
	}

	err = service.UpdateUser(*oldUser, newUser)
	if err != nil {
		return util.NewErrorResponse(err)
	}

	responseBody := ResponseBody{
		User: UserResponse{
			Username: newUser.Username,
			Email:    newUser.Email,
			Image:    newUser.Image,
			Bio:      newUser.Bio,
			Token:    token,
		},
	}

	return util.NewSuccessResponse(200, responseBody)
}

func main() {
	lambda.Start(Handle)
}
