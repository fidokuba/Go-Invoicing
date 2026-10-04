-- Irreversible data change: once restored as Archived, a formerly
-- deleted customer can't be told apart from one archived directly, so
-- this down migration deliberately leaves the data as it is.
SELECT 1;
