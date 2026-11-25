const isJSON = r => /application\/json/i.test(r.headers.get('content-type'));

/* gofastr:html-response-check */
const htmlOnly = (r) => { if (!(r.headers.get('Content-Type') || '').toLowerCase().startsWith('text/html')) throw new Error(`HTTP ${r.status}`); };
