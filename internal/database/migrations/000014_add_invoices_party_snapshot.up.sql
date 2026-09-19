--Milestone 7 Part 2: seller/customer/currency snapshot, captured once by
--InvoiceService.Send at the Draft -> Sent transition and never rewritten
--afterward — see Invoice.MarkSent and InvoicePartySnapshot. Every column
--is nullable at the schema level, including seller_name/customer_name/
--currency (which the application treats as required for any invoice it
--sends going forward): existing Draft rows never populate these, and
--existing Sent/Paid rows created before this migration have no
--historically-accurate snapshot to backfill, so a nullable column is the
--only honest option for them.
ALTER TABLE invoices
    ADD COLUMN seller_name TEXT,
    ADD COLUMN seller_email TEXT,
    ADD COLUMN seller_phone TEXT,
    ADD COLUMN seller_website TEXT,
    ADD COLUMN seller_address TEXT,
    ADD COLUMN seller_city TEXT,
    ADD COLUMN seller_state TEXT,
    ADD COLUMN seller_postal_code TEXT,
    ADD COLUMN seller_country TEXT,
    ADD COLUMN seller_tax_id TEXT,
    ADD COLUMN customer_name TEXT,
    ADD COLUMN customer_company_name TEXT,
    ADD COLUMN customer_email TEXT,
    ADD COLUMN customer_phone TEXT,
    ADD COLUMN customer_tax_id TEXT,
    ADD COLUMN customer_address TEXT,
    ADD COLUMN customer_city TEXT,
    ADD COLUMN customer_state TEXT,
    ADD COLUMN customer_postal_code TEXT,
    ADD COLUMN customer_country TEXT,
    ADD COLUMN currency TEXT;
