# givetypst

An HTTP service that generates PDFs from [Typst](https://typst.app/) templates stored in S3-compatible cloud storage.

## Usage

```text
Usage: givetypst [OPTIONS]

Generate PDFs from Typst templates stored in cloud storage.

Environment Variables:
  BUCKET_URL          URL of the cloud storage bucket containing templates (required)
  PORT                HTTP port to listen on (overrides -port flag)
  MAX_TEMPLATE_SIZE   Maximum template file size in bytes (default: 1048576)
  MAX_DATA_SIZE       Maximum data file size in bytes (default: 10485760)

Options:
  -port int
        HTTP port to listen on (default 8080)
  -v    Verbose output (debug mode)
  -version
        Show version and exit
```

## Why?

Generate PDFs on-demand from templates without managing Typst installations.

Store your Typst templates in S3-compatible storage, then call the API with template data to receive a compiled PDF.
Useful for generating invoices, reports, certificates, or any document from structured data.

## API

### Health Check

```
GET /health
```

Returns `OK` if the service is running and can access the storage bucket.

### Generate PDF

```
POST /generate
Content-Type: application/json
```

The request body supports three modes:

#### Inline Data

Small data can be passed directly in the request:

```json
{
  "templateKey": "invoice.typ",
  "data": {
    "customer": "Acme Corp",
    "amount": "1000.00"
  }
}
```

#### Data from Bucket

Large data (e.g., full resume content) can be stored in the bucket and referenced by key:

```json
{
  "templateKey": "resume.typ",
  "dataKey": "resumes/john-doe.json"
}
```

#### No Data

Templates that don't require external data:

```json
{
  "templateKey": "static-document.typ"
}
```

> **Note:** You cannot specify both `data` and `dataKey` in the same request.

The data (from either source) is written to `data.json` and can be accessed in your template via `#let data = json("data.json")`.

Returns the generated PDF.

## Docker

```bash
docker run -e BUCKET_URL=s3://my-bucket?region=us-east-1 -p 8080:8080 ghcr.io/boringbin/givetypst
```

## Supported Storage

Any S3-compatible storage via [gocloud.dev/blob](https://gocloud.dev/howto/blob/):

- AWS S3
- Google Cloud Storage
- Azure Blob Storage
- MinIO
- And more

## Typst Version

Currently targets [Typst 0.14.2](https://github.com/typst/typst/releases/tag/v0.14.2).

## License

[MIT](LICENSE)
