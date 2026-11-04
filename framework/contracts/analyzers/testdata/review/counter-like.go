package queue

import "fmt"

var statement = `UPDATE jobs SET attempts = CASE WHEN state LIKE '%active%' THEN 1 ELSE 0 END WHERE id = 1`
var timestampPattern = `UPDATE jobs SET attempts = CASE WHEN state LIKE '%T' THEN 1 ELSE 0 END WHERE id = 1`
var stringPattern = `UPDATE jobs SET attempts = CASE WHEN state LIKE '%s' THEN 1 ELSE 0 END WHERE id = 1`
var escapedPattern = fmt.Sprintf(`UPDATE jobs SET attempts = CASE WHEN state LIKE '%%T' THEN 1 ELSE 0 END WHERE id = 1`)
