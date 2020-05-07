/*
 *    Copyright 2019 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package auth

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/dgraph-io/dgraph/graphql/e2e/common"
	"github.com/dgraph-io/dgraph/testutil"
	"github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

const (
	graphqlURL = "http://localhost:8180/graphql"
)

type User struct {
	Username string `json:"username,omitempty"`
	Age      uint64 `json:"age,omitempty"`
	IsPublic bool   `json:"isPublic,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type UserSecret struct {
	Id      string `json:"id,omitempty"`
	ASecret string `json:"aSecret,omitempty"`
	OwnedBy string `json:"ownedBy,omitempty"`
}

type Region struct {
	Id     string  `json:"id,omitempty"`
	Name   string  `json:"name,omitempty"`
	Users  []*User `json:"users,omitempty"`
	Global bool    `json:"global,omitempty"`
}

type Movie struct {
	Id               string    `json:"id,omitempty"`
	Content          string    `json:"content,omitempty"`
	Hidden           bool      `json:"hidden,omitempty"`
	RegionsAvailable []*Region `json:"regionsAvailable,omitempty"`
}

type Issue struct {
	Id    string `json:"id,omitempty"`
	Msg   string `json:"msg,omitempty"`
	Owner *User  `json:"owner,omitempty"`
}

type Log struct {
	Id   string `json:"id,omitempty"`
	Logs string `json:"logs,omitempty"`
}

type Role struct {
	Id         string  `json:"id,omitempty"`
	Permission string  `json:"permission,omitempty"`
	AssignedTo []*User `json:"assignedTo,omitempty"`
}

type Ticket struct {
	Id         string  `json:"id,omitempty"`
	OnColumn   *Column `json:"onColumn,omitempty"`
	Title      string  `json:"title,omitempty"`
	AssignedTo []*User `json:"assignedTo,omitempty"`
}

type Column struct {
	ColID     string    `json:"colID,omitempty"`
	InProject Project   `json:"inProject,omitempty"`
	Name      string    `json:"name,omitempty"`
	Tickets   []*Ticket `json:"tickets,omitempty"`
}

type Project struct {
	ProjID  string    `json:"projID,omitempty"`
	Name    string    `json:"name,omitempty"`
	Roles   []*Role   `json:"roles,omitempty"`
	Columns []*Column `json:"columns,omitempty"`
}

type TestCase struct {
	user      string
	role      string
	result    string
	name      string
	variables map[string]interface{}
}

func getJWT(t *testing.T, user, role string) http.Header {
	type MyCustomClaims struct {
		Foo map[string]interface{} `json:"https://xyz.io/jwt/claims"`
		jwt.StandardClaims
	}

	// Create the Claims
	claims := MyCustomClaims{
		map[string]interface{}{},
		jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Minute * 1000).Unix(),
			Issuer:    "test",
		},
	}

	if user != "" {
		claims.Foo["USER"] = user
	}

	if role != "" {
		claims.Foo["ROLE"] = role
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString([]byte("secretkey"))
	require.NoError(t, err)

	h := make(http.Header)
	h.Add("X-Test-Auth", ss)

	return h
}

func TestOrRBACFilter(t *testing.T) {
	testCases := []TestCase{{
		user: "user1",
		role: "ADMIN",
		result: `{
                            "queryProject": [
                              {
                                "name": "Project1"
                              },
                              {
                                "name": "Project2"
                              }
                            ]
                        }`,
	}, {
		user: "user1",
		role: "USER",
		result: `{
                            "queryProject": [
                              {
                                "name": "Project1"
                              }
                            ]
                        }`,
	}, {
		user: "user4",
		role: "USER",
		result: `{
                            "queryProject": [
                              {
                                "name": "Project2"
                              }
                            ]
                        }`,
	}}

	query := `
            query {
                queryProject (order: {asc: name}) {
			name
		}
	    }
	`

	for _, tcase := range testCases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers: getJWT(t, tcase.user, tcase.role),
				Query:   query,
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func getColID(t *testing.T, tcase TestCase) string {
	query := `
		query($name: String!) {
		    queryColumn(filter: {name: {eq: $name}}) {
		        colID
		    	name
		    }
		}
	`

	var result struct {
		QueryColumn []*Column
	}

	getUserParams := &common.GraphQLParams{
		Headers:   getJWT(t, tcase.user, tcase.role),
		Query:     query,
		Variables: map[string]interface{}{"name": tcase.name},
	}

	gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
	require.Nil(t, gqlResponse.Errors)

	err := json.Unmarshal([]byte(gqlResponse.Data), &result)
	require.Nil(t, err)

	if len(result.QueryColumn) > 0 {
		return result.QueryColumn[0].ColID
	}

	return ""
}

func TestRootGetFilter(t *testing.T) {
	idCol1 := getColID(t, TestCase{"user1", "USER", "", "Column1", nil})
	idCol2 := getColID(t, TestCase{"user2", "USER", "", "Column2", nil})

	require.NotEqual(t, idCol1, "")
	require.NotEqual(t, idCol2, "")

	tcases := []TestCase{{
		user:   "user1",
		role:   "USER",
		result: `{"getColumn": {"name": "Column1"}}`,
		name:   idCol1,
	}, {
		user:   "user1",
		role:   "USER",
		result: `{"getColumn": null}`,
		name:   idCol2,
	}, {
		user:   "user2",
		role:   "USER",
		result: `{"getColumn": {"name": "Column2"}}`,
		name:   idCol2,
	}}

	query := `
		query($id: ID!) {
		    getColumn(colID: $id) {
			name
		    }
		}
	`

	for _, tcase := range tcases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers:   getJWT(t, tcase.user, tcase.role),
				Query:     query,
				Variables: map[string]interface{}{"id": tcase.name},
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestDeepFilter(t *testing.T) {
	tcases := []TestCase{{
		user:   "user1",
		role:   "USER",
		result: `{"queryProject":[{"name":"Project1","columns":[{"name":"Column1"}]}]}`,
		name:   "Column1",
	}, {
		user:   "user2",
		role:   "USER",
		result: `{"queryProject":[{"name":"Project1","columns":[{"name":"Column1"}]}, {"name":"Project2","columns":[]}]}`,
		name:   "Column1",
	}, {
		user:   "user2",
		role:   "USER",
		result: `{"queryProject":[{"name":"Project1","columns":[]}, {"name":"Project2","columns":[{"name":"Column3"}]}]}`,
		name:   "Column3",
	}}

	query := `
		query($name: String!) {
		    queryProject (order: {asc: name}) {
		       name
		       columns (filter: {name: {eq: $name}}, first: 1) {
		       	   name
                       }
                    }
                 }
	`

	for _, tcase := range tcases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers:   getJWT(t, tcase.user, tcase.role),
				Query:     query,
				Variables: map[string]interface{}{"name": tcase.name},
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestRootFilter(t *testing.T) {
	testCases := []TestCase{{
		user:   "user1",
		role:   "USER",
		result: `{"queryColumn": [{"name": "Column1"}]}`,
	}, {
		user:   "user2",
		role:   "USER",
		result: `{"queryColumn": [{"name": "Column1"}, {"name": "Column2"}, {"name": "Column3"}]}`,
	}, {
		user:   "user4",
		role:   "USER",
		result: `{"queryColumn": [{"name": "Column2"}, {"name": "Column3"}]}`,
	}}
	query := `
	query {
		queryColumn(order: {asc: name}) {
			name
		}
	}`

	for _, tcase := range testCases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers: getJWT(t, tcase.user, tcase.role),
				Query:   query,
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestDeepRBACValue(t *testing.T) {
	testCases := []TestCase{
		{user: "user1", role: "USER", result: `{"queryUser": [{"username": "user1", "issues":[]}]}`},
		{user: "user1", role: "ADMIN", result: `{"queryUser":[{"username":"user1","issues":[{"msg":"Issue1"}]}]}`},
	}

	query := `
{
  queryUser (filter:{username:{eq:"user1"}}) {
    username
    issues {
      msg
    }
  }
}
	`

	for _, tcase := range testCases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers: getJWT(t, tcase.user, tcase.role),
				Query:   query,
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestRBACFilter(t *testing.T) {
	testCases := []TestCase{
		{role: "USER", result: `{"queryLog": []}`},
		{result: `{"queryLog": []}`},
		{role: "ADMIN", result: `{"queryLog": [{"logs": "Log1"},{"logs": "Log2"}]}`}}

	query := `
		query {
                    queryLog (order: {asc: logs}) {
		    	logs
		    }
		}
	`

	for _, tcase := range testCases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers: getJWT(t, tcase.user, tcase.role),
				Query:   query,
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestAndRBACFilter(t *testing.T) {
	testCases := []TestCase{{
		user:   "user1",
		role:   "USER",
		result: `{"queryIssue": []}`,
	}, {
		user:   "user2",
		role:   "USER",
		result: `{"queryIssue": []}`,
	}, {
		user:   "user2",
		role:   "ADMIN",
		result: `{"queryIssue": [{"msg": "Issue2"}]}`,
	}}
	query := `
		query {
                    queryIssue (order: {asc: msg}) {
			msg
		    }
		}
	`

	for _, tcase := range testCases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers: getJWT(t, tcase.user, tcase.role),
				Query:   query,
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestNestedFilter(t *testing.T) {
	testCases := []TestCase{{
		user: "user1",
		role: "USER",
		result: `
{
   "queryMovie": [
      {
         "content": "Movie2",
         "regionsAvailable": [
            {
               "name": "Region1"
            }
         ]
      },
      {
         "content": "Movie3",
         "regionsAvailable": [
            {
               "name": "Region1"
            },
            {
               "name": "Region4"
            }
         ]
      }
   ]
}
		`,
	}, {
		user: "user2",
		role: "USER",
		result: `
{
   "queryMovie": [
      {
         "content": "Movie1",
         "regionsAvailable": [
            {
               "name": "Region2"
            },
            {
               "name": "Region3"
            }
         ]
      },
      {
         "content": "Movie2",
         "regionsAvailable": [
            {
               "name": "Region1"
            }
         ]
      },
      {
         "content": "Movie3",
         "regionsAvailable": [
            {
               "name": "Region1"
            },
            {
               "name": "Region4"
            }
         ]
      }
   ]
}
		`,
	}}

	query := `
		query {
                    queryMovie (order: {asc: content}) {
		           content
		           regionsAvailable (order: {asc: name}) {
		           	name
		           }
		    }
		}
	`

	for _, tcase := range testCases {
		t.Run(tcase.role+tcase.user, func(t *testing.T) {
			getUserParams := &common.GraphQLParams{
				Headers: getJWT(t, tcase.user, tcase.role),
				Query:   query,
			}

			gqlResponse := getUserParams.ExecuteAsPost(t, graphqlURL)
			require.Nil(t, gqlResponse.Errors)

			require.JSONEq(t, string(gqlResponse.Data), tcase.result)
		})
	}
}

func TestMain(m *testing.M) {
	schemaFile := "schema.graphql"
	schema, err := ioutil.ReadFile(schemaFile)
	if err != nil {
		panic(err)
	}

	schema, err = testutil.AppendAuthInfo(schema, "HS256")
	if err != nil {
		panic(err)
	}

	jsonFile := "test_data.json"
	data, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		panic(errors.Wrapf(err, "Unable to read file %s.", jsonFile))
	}

	common.BootstrapServer(schema, data)

	os.Exit(m.Run())
}