#!/bin/bash
EMAIL="user_$(head /dev/urandom | tr -dc a-z0-9 | head -c 8)@example.com"
PASSWORD="password123"
NAME="Test User"

echo "1. Signup: $EMAIL"
SIGNUP_RES=$(curl -s -X POST http://localhost:8080/api/auth/signup \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$NAME\", \"email\":\"$EMAIL\", \"password\":\"$PASSWORD\"}")
echo "Signup: $SIGNUP_RES"

echo "2. Login"
LOGIN_RES=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$EMAIL\", \"password\":\"$PASSWORD\"}")
echo "Login: $LOGIN_RES"

TOKEN=$(echo $LOGIN_RES | sed -E 's/.*"token":"([^"]+)".*/\1/' | grep -v '{')
if [[ "$TOKEN" == "$LOGIN_RES" ]]; then
    TOKEN=$(echo $LOGIN_RES | sed -E 's/.*"accessToken":"([^"]+)".*/\1/' | grep -v '{')
fi

if [[ -n "$TOKEN" && "$TOKEN" != "$LOGIN_RES" ]]; then
    echo "3. Get Me"
    ME_RES=$(curl -s -X GET http://localhost:8080/api/auth/me \
      -H "Authorization: Bearer $TOKEN")
    echo "Me: $ME_RES"
    echo "Result: PASS"
else
    echo "Error: Token not found or login failed."
    echo "Result: FAIL"
fi
