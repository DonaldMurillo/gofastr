package queue

var statement = `UPDATE jobs SET attempts = CASE WHEN state LIKE '%active%' THEN 1 ELSE 0 END WHERE id = 1`
