






CREATE TABLE public.calls (
    id bigint NOT NULL,
    call_data bytea,
    stream_id bigint,
    start_time timestamp with time zone,
    duration bigint,
    active boolean,
    user_id bigint,
    repeater_id bigint,
    time_slot boolean,
    group_call boolean,
    is_to_talkgroup boolean,
    to_talkgroup_id bigint,
    is_to_user boolean,
    to_user_id bigint,
    is_to_repeater boolean,
    to_repeater_id bigint,
    destination_id bigint,
    total_packets bigint,
    lost_sequences bigint,
    loss numeric,
    jitter numeric,
    last_frame_num bigint,
    last_seq bigint,
    ber numeric,
    rssi numeric,
    total_bits bigint,
    total_errors bigint,
    last_packet_time timestamp with time zone,
    has_header boolean,
    has_term boolean,
    kind character varying(16) DEFAULT 'voice'::character varying NOT NULL,
    sub_kind character varying(64),
    source_id bigint,
    transport character varying(16),
    ingress_id bigint,
    bursts bigint,
    f_id smallint,
    opcode smallint,
    sap smallint,
    data_format character varying(16),
    confirmed boolean,
    blocks bigint,
    octets bigint,
    bad_crc bigint,
    lost_blocks bigint,
    response character varying(8),
    talker_alias character varying(64),
    alias_format character varying(16),
    latitude numeric,
    longitude numeric,
    position_error smallint,
    partial boolean,
    undeliverable text,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    codec character varying(8),
    codec2_mode bigint,
    sent_ambe bigint,
    sent_codec2 bigint,
    missed_ms bigint,
    transcode_busy boolean
);



CREATE SEQUENCE public.calls_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.calls_id_seq OWNED BY public.calls.id;



CREATE TABLE public.messages (
    id bigint NOT NULL,
    received_at timestamp with time zone NOT NULL,
    transmission_id bigint,
    source_id bigint NOT NULL,
    destination_id bigint NOT NULL,
    is_group boolean DEFAULT false NOT NULL,
    repeater_id bigint DEFAULT 0 NOT NULL,
    slot smallint DEFAULT 1 NOT NULL,
    transport character varying(16),
    stream_id bigint DEFAULT 0 NOT NULL,
    sap smallint,
    format character varying(16) NOT NULL,
    confirmed boolean DEFAULT false NOT NULL,
    send_sequence smallint,
    fragment_seq smallint,
    response character varying(8),
    bad_crc boolean DEFAULT false NOT NULL,
    text text,
    checksum_ok boolean NOT NULL,
    blocks bigint DEFAULT 0 NOT NULL,
    octets bigint DEFAULT 0 NOT NULL,
    raw bytea,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);



CREATE SEQUENCE public.messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.messages_id_seq OWNED BY public.messages.id;



CREATE TABLE public.migrations (
    id character varying(255) NOT NULL
);



CREATE TABLE public.nets (
    id bigint NOT NULL,
    talkgroup_id bigint NOT NULL,
    started_by_user_id bigint,
    scheduled_net_id bigint,
    start_time timestamp with time zone,
    end_time timestamp with time zone,
    duration_minutes bigint,
    description text,
    active boolean,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    showcase boolean DEFAULT false
);



CREATE SEQUENCE public.nets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.nets_id_seq OWNED BY public.nets.id;



CREATE TABLE public.o_auth_codes (
    hash text NOT NULL,
    user_id bigint,
    client_id text,
    redirect_uri text,
    scopes text,
    challenge text,
    expires_at timestamp with time zone,
    used_at timestamp with time zone,
    grant_id text
);



CREATE TABLE public.o_auth_grants (
    id text NOT NULL,
    user_id bigint,
    client_id text,
    scopes text,
    created_at timestamp with time zone,
    last_used_at timestamp with time zone,
    revoked_at timestamp with time zone
);



CREATE TABLE public.o_auth_refresh_tokens (
    hash text NOT NULL,
    grant_id text,
    expires_at timestamp with time zone,
    used_at timestamp with time zone
);



CREATE TABLE public.password_resets (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone
);



CREATE SEQUENCE public.password_resets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.password_resets_id_seq OWNED BY public.password_resets.id;



CREATE TABLE public.peer_rules (
    id bigint NOT NULL,
    peer_id bigint,
    direction boolean,
    subject_id_min bigint,
    subject_id_max bigint,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone
);



CREATE SEQUENCE public.peer_rules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.peer_rules_id_seq OWNED BY public.peer_rules.id;



CREATE TABLE public.peers (
    id bigint NOT NULL,
    last_ping timestamp with time zone,
    ip text,
    port bigint,
    password text,
    owner_id bigint,
    ingress boolean,
    egress boolean,
    enhanced boolean DEFAULT false,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    codec2 boolean DEFAULT false
);



CREATE SEQUENCE public.peers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.peers_id_seq OWNED BY public.peers.id;



CREATE TABLE public.plugin_hosts (
    id bigint NOT NULL,
    plugin_id bigint NOT NULL,
    address character varying(255) NOT NULL,
    reason character varying(200) DEFAULT ''::character varying NOT NULL,
    allowed boolean DEFAULT false NOT NULL
);



CREATE SEQUENCE public.plugin_hosts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.plugin_hosts_id_seq OWNED BY public.plugin_hosts.id;



CREATE TABLE public.plugin_modules (
    plugin_id bigint NOT NULL,
    hash bytea NOT NULL,
    wasm bytea NOT NULL,
    created_at timestamp with time zone
);



CREATE TABLE public.plugin_ranges (
    id bigint NOT NULL,
    plugin_id bigint NOT NULL,
    first_id bigint NOT NULL,
    last_id bigint NOT NULL
);



CREATE SEQUENCE public.plugin_ranges_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.plugin_ranges_id_seq OWNED BY public.plugin_ranges.id;



CREATE TABLE public.plugins (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    token_hash bytea NOT NULL,
    token_created_at timestamp with time zone NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    last_connected_at timestamp with time zone,
    last_seen_at timestamp with time zone,
    last_address character varying(64),
    session character varying(32) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    kind character varying(16) DEFAULT 'external'::character varying NOT NULL,
    builtin character varying(32) DEFAULT ''::character varying NOT NULL,
    version character varying(32) DEFAULT ''::character varying NOT NULL,
    author character varying(64) DEFAULT ''::character varying NOT NULL,
    description character varying(500) DEFAULT ''::character varying NOT NULL,
    module_hash bytea
);



CREATE SEQUENCE public.plugins_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.plugins_id_seq OWNED BY public.plugins.id;



CREATE TABLE public.position_settings (
    radio_id bigint NOT NULL,
    user_id bigint NOT NULL,
    tracking boolean DEFAULT false NOT NULL,
    tracking_since timestamp with time zone,
    interval_seconds bigint DEFAULT 600 NOT NULL,
    retention_hours bigint DEFAULT 24 NOT NULL,
    aprs boolean DEFAULT false NOT NULL,
    aprsss_id character varying(2),
    aprs_symbol character varying(16),
    aprs_comment character varying(64),
    updated_at timestamp with time zone
);



CREATE TABLE public.positions (
    id bigint NOT NULL,
    radio_id bigint NOT NULL,
    latitude numeric NOT NULL,
    longitude numeric NOT NULL,
    format character varying(8) NOT NULL,
    accuracy_m numeric,
    speed_mps numeric,
    heading_deg numeric,
    altitude_m numeric,
    fix_time timestamp with time zone,
    received_at timestamp with time zone NOT NULL,
    repeater_id bigint DEFAULT 0 NOT NULL,
    slot smallint DEFAULT 1 NOT NULL,
    service_id bigint DEFAULT 0 NOT NULL
);



CREATE SEQUENCE public.positions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.positions_id_seq OWNED BY public.positions.id;



CREATE TABLE public.ratelimits (
    key text NOT NULL,
    hits bigint,
    "timestamp" timestamp with time zone
);



CREATE TABLE public.repeater_ts1_static_talkgroups (
    repeater_id bigint NOT NULL,
    talkgroup_id bigint NOT NULL
);



CREATE TABLE public.repeater_ts2_static_talkgroups (
    repeater_id bigint NOT NULL,
    talkgroup_id bigint NOT NULL
);



CREATE TABLE public.repeaters (
    connected timestamp with time zone,
    last_ping timestamp with time zone,
    password text,
    type text DEFAULT 'mmdvm'::text,
    dialect text,
    ts1_dynamic_talkgroup_id bigint,
    ts2_dynamic_talkgroup_id bigint,
    owner_id bigint,
    simplex_repeater boolean,
    trusted boolean DEFAULT false,
    city text,
    state text,
    country text,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    callsign text,
    id bigint NOT NULL,
    rx_frequency bigint,
    tx_frequency bigint,
    tx_power smallint,
    color_code smallint,
    latitude numeric,
    longitude numeric,
    height integer,
    location text,
    description text,
    url text,
    software_id text,
    package_id text,
    slots bigint,
    ts1_codec character varying(8) DEFAULT 'auto'::character varying NOT NULL,
    ts2_codec character varying(8) DEFAULT 'auto'::character varying NOT NULL,
    ts1_heard_codec character varying(8) DEFAULT ''::character varying NOT NULL,
    ts2_heard_codec character varying(8) DEFAULT ''::character varying NOT NULL,
    ts1_heard_codec2_mode bigint DEFAULT 0 NOT NULL,
    ts2_heard_codec2_mode bigint DEFAULT 0 NOT NULL,
    ts1_heard_at timestamp with time zone,
    ts2_heard_at timestamp with time zone
);



CREATE SEQUENCE public.repeaters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.repeaters_id_seq OWNED BY public.repeaters.id;



CREATE TABLE public.scheduled_nets (
    id bigint NOT NULL,
    talkgroup_id bigint NOT NULL,
    created_by_user_id bigint,
    name text,
    description text,
    cron_expression text,
    day_of_week bigint,
    time_of_day text,
    timezone text,
    duration_minutes bigint,
    enabled boolean DEFAULT true,
    next_run timestamp with time zone,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    showcase boolean DEFAULT false
);



CREATE SEQUENCE public.scheduled_nets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.scheduled_nets_id_seq OWNED BY public.scheduled_nets.id;



CREATE TABLE public.sessions (
    id text NOT NULL,
    data text,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    expires_at timestamp with time zone,
    user_id bigint
);



CREATE TABLE public.talkgroup_admins (
    talkgroup_id bigint NOT NULL,
    user_id bigint NOT NULL
);



CREATE TABLE public.talkgroup_ncos (
    talkgroup_id bigint NOT NULL,
    user_id bigint NOT NULL
);



CREATE TABLE public.talkgroups (
    id bigint NOT NULL,
    name text,
    description text,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone
);



CREATE SEQUENCE public.talkgroups_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.talkgroups_id_seq OWNED BY public.talkgroups.id;



CREATE TABLE public.user_radio_ids (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    radio_id bigint NOT NULL,
    verified_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone
);



CREATE SEQUENCE public.user_radio_ids_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.user_radio_ids_id_seq OWNED BY public.user_radio_ids.id;



CREATE TABLE public.users (
    id bigint NOT NULL,
    callsign text,
    username text,
    password text,
    admin boolean,
    super_admin boolean,
    email text,
    approved boolean,
    suspended boolean,
    sms_storage_enabled boolean DEFAULT false NOT NULL,
    sms_storage_since timestamp with time zone,
    share_positions boolean DEFAULT false NOT NULL,
    must_change_password boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone
);



CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;



ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;



ALTER TABLE ONLY public.calls ALTER COLUMN id SET DEFAULT nextval('public.calls_id_seq'::regclass);



ALTER TABLE ONLY public.messages ALTER COLUMN id SET DEFAULT nextval('public.messages_id_seq'::regclass);



ALTER TABLE ONLY public.nets ALTER COLUMN id SET DEFAULT nextval('public.nets_id_seq'::regclass);



ALTER TABLE ONLY public.password_resets ALTER COLUMN id SET DEFAULT nextval('public.password_resets_id_seq'::regclass);



ALTER TABLE ONLY public.peer_rules ALTER COLUMN id SET DEFAULT nextval('public.peer_rules_id_seq'::regclass);



ALTER TABLE ONLY public.peers ALTER COLUMN id SET DEFAULT nextval('public.peers_id_seq'::regclass);



ALTER TABLE ONLY public.plugin_hosts ALTER COLUMN id SET DEFAULT nextval('public.plugin_hosts_id_seq'::regclass);



ALTER TABLE ONLY public.plugin_ranges ALTER COLUMN id SET DEFAULT nextval('public.plugin_ranges_id_seq'::regclass);



ALTER TABLE ONLY public.plugins ALTER COLUMN id SET DEFAULT nextval('public.plugins_id_seq'::regclass);



ALTER TABLE ONLY public.positions ALTER COLUMN id SET DEFAULT nextval('public.positions_id_seq'::regclass);



ALTER TABLE ONLY public.repeaters ALTER COLUMN id SET DEFAULT nextval('public.repeaters_id_seq'::regclass);



ALTER TABLE ONLY public.scheduled_nets ALTER COLUMN id SET DEFAULT nextval('public.scheduled_nets_id_seq'::regclass);



ALTER TABLE ONLY public.talkgroups ALTER COLUMN id SET DEFAULT nextval('public.talkgroups_id_seq'::regclass);



ALTER TABLE ONLY public.user_radio_ids ALTER COLUMN id SET DEFAULT nextval('public.user_radio_ids_id_seq'::regclass);



ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);



ALTER TABLE ONLY public.calls
    ADD CONSTRAINT calls_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.migrations
    ADD CONSTRAINT migrations_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.nets
    ADD CONSTRAINT nets_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.o_auth_codes
    ADD CONSTRAINT o_auth_codes_pkey PRIMARY KEY (hash);



ALTER TABLE ONLY public.o_auth_grants
    ADD CONSTRAINT o_auth_grants_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.o_auth_refresh_tokens
    ADD CONSTRAINT o_auth_refresh_tokens_pkey PRIMARY KEY (hash);



ALTER TABLE ONLY public.password_resets
    ADD CONSTRAINT password_resets_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.peer_rules
    ADD CONSTRAINT peer_rules_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.peers
    ADD CONSTRAINT peers_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.plugin_hosts
    ADD CONSTRAINT plugin_hosts_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.plugin_modules
    ADD CONSTRAINT plugin_modules_pkey PRIMARY KEY (plugin_id);



ALTER TABLE ONLY public.plugin_ranges
    ADD CONSTRAINT plugin_ranges_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.plugins
    ADD CONSTRAINT plugins_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.position_settings
    ADD CONSTRAINT position_settings_pkey PRIMARY KEY (radio_id);



ALTER TABLE ONLY public.positions
    ADD CONSTRAINT positions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.ratelimits
    ADD CONSTRAINT ratelimits_pkey PRIMARY KEY (key);



ALTER TABLE ONLY public.repeater_ts1_static_talkgroups
    ADD CONSTRAINT repeater_ts1_static_talkgroups_pkey PRIMARY KEY (repeater_id, talkgroup_id);



ALTER TABLE ONLY public.repeater_ts2_static_talkgroups
    ADD CONSTRAINT repeater_ts2_static_talkgroups_pkey PRIMARY KEY (repeater_id, talkgroup_id);



ALTER TABLE ONLY public.repeaters
    ADD CONSTRAINT repeaters_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.scheduled_nets
    ADD CONSTRAINT scheduled_nets_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.talkgroup_admins
    ADD CONSTRAINT talkgroup_admins_pkey PRIMARY KEY (talkgroup_id, user_id);



ALTER TABLE ONLY public.talkgroup_ncos
    ADD CONSTRAINT talkgroup_ncos_pkey PRIMARY KEY (talkgroup_id, user_id);



ALTER TABLE ONLY public.talkgroups
    ADD CONSTRAINT talkgroups_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.user_radio_ids
    ADD CONSTRAINT user_radio_ids_pkey PRIMARY KEY (id);



ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);



CREATE INDEX idx_calls_deleted_at ON public.calls USING btree (deleted_at);



CREATE INDEX idx_calls_kind ON public.calls USING btree (kind);



CREATE INDEX idx_calls_source_id ON public.calls USING btree (source_id);



CREATE INDEX idx_messages_destination_received ON public.messages USING btree (destination_id, received_at);



CREATE INDEX idx_messages_format ON public.messages USING btree (format);



CREATE INDEX idx_messages_received_at ON public.messages USING btree (received_at);



CREATE INDEX idx_messages_source_received ON public.messages USING btree (source_id, received_at);



CREATE INDEX idx_messages_transmission_id ON public.messages USING btree (transmission_id);



CREATE INDEX idx_nets_active ON public.nets USING btree (active);



CREATE INDEX idx_nets_deleted_at ON public.nets USING btree (deleted_at);



CREATE UNIQUE INDEX idx_nets_one_active_per_tg ON public.nets USING btree (talkgroup_id) WHERE (active = true);



CREATE INDEX idx_nets_talkgroup_id ON public.nets USING btree (talkgroup_id);



CREATE INDEX idx_o_auth_codes_expires_at ON public.o_auth_codes USING btree (expires_at);



CREATE INDEX idx_o_auth_grants_client_id ON public.o_auth_grants USING btree (client_id);



CREATE INDEX idx_o_auth_grants_revoked_at ON public.o_auth_grants USING btree (revoked_at);



CREATE INDEX idx_o_auth_grants_user_id ON public.o_auth_grants USING btree (user_id);



CREATE INDEX idx_o_auth_refresh_tokens_grant_id ON public.o_auth_refresh_tokens USING btree (grant_id);



CREATE INDEX idx_password_resets_expires_at ON public.password_resets USING btree (expires_at);



CREATE UNIQUE INDEX idx_password_resets_token_hash ON public.password_resets USING btree (token_hash);



CREATE INDEX idx_password_resets_user_id ON public.password_resets USING btree (user_id);



CREATE INDEX idx_peer_rules_deleted_at ON public.peer_rules USING btree (deleted_at);



CREATE INDEX idx_peers_deleted_at ON public.peers USING btree (deleted_at);



CREATE INDEX idx_plugin_hosts_plugin_id ON public.plugin_hosts USING btree (plugin_id);



CREATE INDEX idx_plugin_ranges_plugin_id ON public.plugin_ranges USING btree (plugin_id);



CREATE UNIQUE INDEX idx_plugins_name ON public.plugins USING btree (name);



CREATE UNIQUE INDEX idx_plugins_token_hash ON public.plugins USING btree (token_hash);



CREATE INDEX idx_position_settings_user_id ON public.position_settings USING btree (user_id);



CREATE INDEX idx_positions_radio_received ON public.positions USING btree (radio_id, received_at);



CREATE INDEX idx_positions_received_at ON public.positions USING btree (received_at);



CREATE INDEX idx_repeaters_deleted_at ON public.repeaters USING btree (deleted_at);



CREATE INDEX idx_scheduled_nets_deleted_at ON public.scheduled_nets USING btree (deleted_at);



CREATE INDEX idx_scheduled_nets_talkgroup_id ON public.scheduled_nets USING btree (talkgroup_id);



CREATE INDEX idx_sessions_expires_at ON public.sessions USING btree (expires_at);



CREATE INDEX idx_sessions_user_id ON public.sessions USING btree (user_id);



CREATE INDEX idx_talkgroups_deleted_at ON public.talkgroups USING btree (deleted_at);



CREATE UNIQUE INDEX idx_user_radio_ids_radio_id ON public.user_radio_ids USING btree (radio_id);



CREATE INDEX idx_user_radio_ids_user_id ON public.user_radio_ids USING btree (user_id);



CREATE UNIQUE INDEX idx_users_callsign ON public.users USING btree (callsign);



CREATE INDEX idx_users_deleted_at ON public.users USING btree (deleted_at);



CREATE UNIQUE INDEX idx_users_username ON public.users USING btree (username);



ALTER TABLE ONLY public.calls
    ADD CONSTRAINT fk_calls_repeater FOREIGN KEY (repeater_id) REFERENCES public.repeaters(id);



ALTER TABLE ONLY public.calls
    ADD CONSTRAINT fk_calls_to_repeater FOREIGN KEY (to_repeater_id) REFERENCES public.repeaters(id);



ALTER TABLE ONLY public.calls
    ADD CONSTRAINT fk_calls_to_talkgroup FOREIGN KEY (to_talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.calls
    ADD CONSTRAINT fk_calls_to_user FOREIGN KEY (to_user_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.calls
    ADD CONSTRAINT fk_calls_user FOREIGN KEY (user_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.messages
    ADD CONSTRAINT fk_messages_transmission FOREIGN KEY (transmission_id) REFERENCES public.calls(id) ON DELETE SET NULL;



ALTER TABLE ONLY public.peer_rules
    ADD CONSTRAINT fk_peer_rules_peer FOREIGN KEY (peer_id) REFERENCES public.peers(id);



ALTER TABLE ONLY public.peers
    ADD CONSTRAINT fk_peers_owner FOREIGN KEY (owner_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.plugin_ranges
    ADD CONSTRAINT fk_plugins_ranges FOREIGN KEY (plugin_id) REFERENCES public.plugins(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.repeater_ts1_static_talkgroups
    ADD CONSTRAINT fk_repeater_ts1_static_talkgroups_repeater FOREIGN KEY (repeater_id) REFERENCES public.repeaters(id);



ALTER TABLE ONLY public.repeater_ts1_static_talkgroups
    ADD CONSTRAINT fk_repeater_ts1_static_talkgroups_talkgroup FOREIGN KEY (talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.repeater_ts2_static_talkgroups
    ADD CONSTRAINT fk_repeater_ts2_static_talkgroups_repeater FOREIGN KEY (repeater_id) REFERENCES public.repeaters(id);



ALTER TABLE ONLY public.repeater_ts2_static_talkgroups
    ADD CONSTRAINT fk_repeater_ts2_static_talkgroups_talkgroup FOREIGN KEY (talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.repeaters
    ADD CONSTRAINT fk_repeaters_ts1_dynamic_talkgroup FOREIGN KEY (ts1_dynamic_talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.repeaters
    ADD CONSTRAINT fk_repeaters_ts2_dynamic_talkgroup FOREIGN KEY (ts2_dynamic_talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.talkgroup_admins
    ADD CONSTRAINT fk_talkgroup_admins_talkgroup FOREIGN KEY (talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.talkgroup_admins
    ADD CONSTRAINT fk_talkgroup_admins_user FOREIGN KEY (user_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.talkgroup_ncos
    ADD CONSTRAINT fk_talkgroup_ncos_talkgroup FOREIGN KEY (talkgroup_id) REFERENCES public.talkgroups(id);



ALTER TABLE ONLY public.talkgroup_ncos
    ADD CONSTRAINT fk_talkgroup_ncos_user FOREIGN KEY (user_id) REFERENCES public.users(id);



ALTER TABLE ONLY public.user_radio_ids
    ADD CONSTRAINT fk_user_radio_ids_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;



ALTER TABLE ONLY public.repeaters
    ADD CONSTRAINT fk_users_repeaters FOREIGN KEY (owner_id) REFERENCES public.users(id);





INSERT INTO migrations (id) VALUES ('202302242025');
INSERT INTO migrations (id) VALUES ('202311190252');
INSERT INTO migrations (id) VALUES ('202506150100');
INSERT INTO migrations (id) VALUES ('202602100435');
INSERT INTO migrations (id) VALUES ('202602100457');
INSERT INTO migrations (id) VALUES ('202602130100');
INSERT INTO migrations (id) VALUES ('202602160100');
INSERT INTO migrations (id) VALUES ('202602160200');
INSERT INTO migrations (id) VALUES ('202602160300');
INSERT INTO migrations (id) VALUES ('202602170100');
INSERT INTO migrations (id) VALUES ('202602170200');
INSERT INTO migrations (id) VALUES ('202602170300');
INSERT INTO migrations (id) VALUES ('202610221900');
INSERT INTO migrations (id) VALUES ('202610221910');
INSERT INTO migrations (id) VALUES ('202610222000');
INSERT INTO migrations (id) VALUES ('202610230000');
INSERT INTO migrations (id) VALUES ('202610231200');
INSERT INTO migrations (id) VALUES ('202610232100');
INSERT INTO migrations (id) VALUES ('202610240100');
INSERT INTO migrations (id) VALUES ('202610240200');
INSERT INTO migrations (id) VALUES ('202610241000');
INSERT INTO migrations (id) VALUES ('202610242000');
INSERT INTO migrations (id) VALUES ('202610242100');
INSERT INTO migrations (id) VALUES ('202610251000');
INSERT INTO migrations (id) VALUES ('202610251010');
INSERT INTO migrations (id) VALUES ('202610251100');
INSERT INTO migrations (id) VALUES ('202610261000');
INSERT INTO migrations (id) VALUES ('202610261100');
INSERT INTO migrations (id) VALUES ('202610261200');
