export default function MatchCard({ match }) {
  return (
    <div className="bg-white p-4 border-b border-gray-100 flex items-center justify-between">
      <div className="flex-1 text-right pr-4">
        <span className="font-medium">{match.homeTeam}</span>
      </div>

      <div className="flex flex-col items-center justify-center w-20">
        <div className="bg-gray-50 px-2 py-1 rounded text-sm font-bold">
          {match.homeScore !== null ? `${match.homeScore} - ${match.awayScore}` : match.time}
        </div>
        <span className={`text-[10px] mt-1 ${match.status === 'በቀጥታ' ? 'text-red-500' : 'text-gray-400'}`}>
          {match.status}
        </span>
      </div>

      <div className="flex-1 text-left pl-4">
        <span className="font-medium">{match.awayTeam}</span>
      </div>
    </div>
  );
}
