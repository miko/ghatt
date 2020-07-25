Feature: Query countries
  As a graphql developer
  I want to test ghatt with sample countries database
  So I know how to test graphql

  Background: Set up
    Given I remember "GRAPHQL_ENDPOINT" as "https://countries.trevorblades.com/"
    And I remember "COUNTRIES_LIST" as:
    """
query {
  countries {
    code
    capital
    name
  }
}
    """
    And I remember "COUNTRY_BY_CODE" as:
    """
query ($code:String!="US") {
  countries (filter:{code:{eq:$code}}) {
    capital
    name
   continent {name}
  }
}
    """

  Scenario: Get code, capital and name of all countries
    When I execute query "COUNTRIES_LIST"
    Then the response code should be 200
    And the response jq ".data.countries|length" should match number "250"

  Scenario: Get info abount Poland by code
    Given I set variable "code" as "PL"
    When I execute query "COUNTRY_BY_CODE"
    Then the response code should be 200
    And I dump response as JSON
    And the response should match subset of json:
    """
    {"data":{
    "countries":[
    {"name":"Poland", "capital":"Warsaw", "continent":{"name":"Europe"}}
    ]
      }}
    """
