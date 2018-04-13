		if KEYS[1] == nil then
			return error("spcode empty")
		end

		local spcode = KEYS[1]

		local stampkey = "stamp."..KEYS[2]
		local stamp = redis.call("GET","stamp."..KEYS[2])

		 if stamp == KEYS[2] then 
		 	return "data has saved ok"
		 end


		--local spkey = "service."..KEYS[1]

		local v = redis.call("GET",KEYS[2])
		


		if v==nil then 
			return error("invalid keys[2]")
		end
			
		local services = cjson.decode(v)

		if type(services) ~= "table" then
			return error ("data is not a Array")
		end

		redis.call("DEL","services:"..spcode)

		for k,service  in  ipairs (services) do 

			local mainkey = "service:"..spcode..":"..service.servicecode

			if service.servicecode ~= nil then
				redis.call("HSET",mainkey,"servicecode",service.servicecode)

				if service.servicename ~=nil then 

					redis.call("HSET",mainkey,"servicename",service.servicename)
				end

				if service.servicedesc ~=nil then 

					redis.call("HSET",mainkey,"servicedesc",service.servicedesc)
				end
				if service.signature ~=nil then 

					redis.call("HSET",mainkey,"signature",service.signature)
				end
				if service.status ~=nil then 

					redis.call("HSET",mainkey,"status",service.status)
				end
				if service.type ~=nil then 

					redis.call("HSET",mainkey,"type",service.type)
				end
				if service.feetype ~=nil then 

					redis.call("HSET",mainkey,"feetype",service.feetype)
				end

				if service.feecode ~=nil then 

					redis.call("HSET",mainkey,"feecode",service.feecode)
				end
				if service.credit ~=nil then 

					redis.call("HSET",mainkey,"credit",service.credit)
				end
				if service.timecontrol ~=nil then 

					redis.call("HSET",mainkey,"timecontrol",service.timecontrol)
				end
				redis.call("SADD","services:"..spcode,service.servicecode)

			end

		end
		--标记命令时间戳
		redis.call("SET",stampkey,KEYS[2])
		redis.call("EXPIRE",stampkey,60)

		return "set ok"


