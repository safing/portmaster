package client

// Get sends a get command to the API.
func (c *Client) Get(key string, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestGet, key, nil)
	return op
}

// Query sends a query command to the API.
func (c *Client) Query(query string, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestQuery, query, nil)
	return op
}

// Sub sends a sub command to the API.
func (c *Client) Sub(query string, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestSub, query, nil)
	return op
}

// Qsub sends a qsub command to the API.
func (c *Client) Qsub(query string, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestQsub, query, nil)
	return op
}

// Create sends a create command to the API.
func (c *Client) Create(key string, value interface{}, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestCreate, key, value)
	return op
}

// Update sends an update command to the API.
func (c *Client) Update(key string, value interface{}, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestUpdate, key, value)
	return op
}

// Insert sends an insert command to the API.
func (c *Client) Insert(key string, value interface{}, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestInsert, key, value)
	return op
}

// Delete sends a delete command to the API.
func (c *Client) Delete(key string, handleFunc func(*Message)) *Operation {
	op := c.NewOperation(handleFunc)
	op.Send(msgRequestDelete, key, nil)
	return op
}
