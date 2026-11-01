-- The identify match held 80 characters (0009 copied the engine's
-- reference schema). D-76 lets a trunk carry up to 20 published ranges,
-- and the carrier ingress (pbx-carrier-ingress) the union of every
-- trunk's: both overflow 80. The engine reads the column as a string of
-- any length.
ALTER TABLE ps_endpoint_id_ips ALTER COLUMN "match" TYPE TEXT;
