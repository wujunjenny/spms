-- KEYS[1] usernumber KEYS[2] spcode ARGV[1] msgcount ARGV[2] maxspcount ARGV[3]maxglobalount
-- return 0 OK -1 usercountmax  -2 spcountmax -3 userblack  -4 spblack

local  userNo = KEYS[1]
local  spcode = KEYS[2]
if userNo == nil then
	return error("userNo is nil ")
end

local  msgcount = ARGV[1]
local  maxsp = ARGV[2]
local  max = ARGV[3]
local  timeout = ARGV[4]



local userkey = "user:"..userNo

local usercount = redis.call("GET",userkey)
if usercount ~= false then 
	usercount = tonumber(usercount)
	if usercount == -1 then
		return  0 
	end

	if usercount == -2 then
		return -3
	end

	if tonumber(msgcount+usercount) > tonumber(max) then
		return -1 
	end

else

	redis.call("SETEX",userkey,timeout,0)
end


if spcode ~=nil then
	local spkey = "user:"..userNo..":"..spcode
	local spcount = redis.call("GET",spkey)
	if spcount ~=false then
		if tonumber(spcount) == -1 then
			return 0
		end

		if tonumber(spcount) == -2 then
			return -4
		end

		if tonumber(spcount)+tonumber(msgcount) > tonumber(maxsp) then
			return -2
		end
	else
		redis.call("SETEX",spkey,timeout,0)
	end

	redis.call("INCRBY",spkey,msgcount)
end


redis.call("INCRBY",userkey,msgcount)


return 0
