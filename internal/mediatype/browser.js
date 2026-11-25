const isJSON = r => /application\/json/i.test(r.headers.get('content-type'));
