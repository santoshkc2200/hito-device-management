# Filling in the import spreadsheets

Fill these in Excel/Google Sheets/LibreOffice, then export as CSV (comma-separated,
UTF-8) before running the import command. Keep the header row exactly as it is.

## devices.csv

| Column | Required? | Notes |
|---|---|---|
| `asset_tag` | **Yes** | The sticker code on the device, e.g. `LAPTOP-07`. This is what matches a row to an existing device on a second import — change it and the tool will create a new device instead of updating the old one. |
| `name` | **Yes** | A human-readable name, e.g. `Dell Latitude 5420`. |
| `category` | **Yes** | A category name, e.g. `Laptop`, `USB Drive`. New categories are created automatically. |
| `manufacturer` | No | |
| `model` | No | |
| `serial_no` | No | |
| `home_location` | No | Where it lives when not on loan, e.g. `Equipment Desk`. |
| `notes` | No | |
| `acquired_on` | No | Date the hospital acquired it, format `YYYY-MM-DD` (e.g. `2025-03-14`). Leave blank if unknown. |

## staff.csv

| Column | Required? | Notes |
|---|---|---|
| `employee_no` | **Yes** | The hospital's employee number. This is what matches a row to an existing staff member on a second import — it is never changed by an import. |
| `full_name` | **Yes** | |
| `department` | No | New departments are created automatically. |
| `email` | No | |
| `phone` | No | |
| `notes` | No | |

## Running the import

```
hdms-cli import devices --file devices.csv --dry-run   # check the report first, nothing is written
hdms-cli import devices --file devices.csv              # actually import
hdms-cli import devices --file devices.csv --mint-credentials   # also print a QR token for each new device
```

Same flags for `hdms-cli import users --file staff.csv`.

Running the same file twice is safe — a second run updates the rows that already
exist instead of duplicating them, matched by `asset_tag` or `employee_no`.

`--mint-credentials` prints each new row's token to the screen once. For a
device it can be recovered again later ("reprint" in the admin console); for
a staff card it cannot — write it down or bind a pre-printed blank card at
registration instead.
