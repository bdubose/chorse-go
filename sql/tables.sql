
create table discord_user
( id text -- ? idk discord calls this a snowflake
, global_name text
, avatar text
, last_sign_in timestamptz default (now() at time zone 'utc')
);