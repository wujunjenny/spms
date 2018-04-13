	if KEYS[1] == nil then
		return error("spcode empty")		
	end

	local spcode = KEYS[1]

	local stampkey = "stamp."..KEYS[2]
	local stamp = redis.call("GET","stamp."..KEYS[2])

	if stamp == KEYS[2] then 
	 	return "data has saved ok"
	end

	local v = redis.call("GET",KEYS[2])
		

	if v==nil then 
		return error("invalid keys[2]")
	end


	local contents = cjson.decode(v)

	if type(contents) ~= "table" then
		return error ("data is not a Array")
	end


	local mainkey = "contents:"..KEYS[1]
	local args = {}
	for k,content in ipairs(contents) do

		if content.pattern ~= nil and content.option ~= nil then

			table.insert(args,cjson.encode(content))
		end
	end

	redis.call("DEL",mainkey)
	local rt = redis.call("SADD",mainkey,unpack(args))

	local  tb = {}

	 
	 table.insert(tb,"result")
	 table.insert(tb,"ok")
	 table.insert(tb,"num")
	 table.insert(tb,string.format("%d",rt))

	return tb